package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"dzforum/internal/mailtest"
	"github.com/jackc/pgx/v5/pgxpool"
)

type receipt struct {
	MessageID string `json:"messageId"`
	To        string `json:"to"`
	Subject   string `json:"subject"`
}

func serveSMTP(ctx context.Context, dir string) error {
	server, err := mailtest.Listen()
	if err != nil {
		return err
	}
	defer server.Close()
	output, err := os.OpenFile(filepath.Join(dir, "smtp.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer output.Close()
	encoder := json.NewEncoder(output)
	if err = os.WriteFile(filepath.Join(dir, "smtp-port"), []byte(strconv.Itoa(server.Port)), 0600); err != nil {
		return err
	}
	record := func(body string) error {
		msg, err := mail.ReadMessage(strings.NewReader(body))
		if err != nil {
			return err
		}
		if err = encoder.Encode(receipt{msg.Header.Get("Message-ID"), msg.Header.Get("To"), msg.Header.Get("Subject")}); err != nil {
			return err
		}
		return output.Sync()
	}
	for {
		select {
		case body := <-server.Messages:
			if err = record(body); err != nil {
				return err
			}
		case <-ctx.Done():
			server.Close()
			for {
				select {
				case body := <-server.Messages:
					if err = record(body); err != nil {
						return err
					}
				default:
					return nil
				}
			}
		}
	}
}

func readSMTPEndpoint(dir string) (int, error) {
	if dir == "" {
		return 0, fmt.Errorf("RECOVERY_SMTP_DIR is required")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "smtp-port"))
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(string(raw))
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid loopback SMTP port")
	}
	return port, nil
}

func verifySMTP(ctx context.Context, pool *pgxpool.Pool, target string) error {
	var id int64
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT id,attempts FROM email_jobs`).Scan(&id, &attempts); err != nil {
		return err
	}
	// Other shared workers can also be interrupted during the selected fault.
	// SMTP is at-least-once; a retry that failed before acceptance adds an attempt
	// without a captured message. The dedicated email fault has an exact window.
	if attempts < 1 || (target == "email" && attempts != 2) {
		return fmt.Errorf("unexpected email attempts=%d target=%s", attempts, target)
	}
	f, err := os.Open(filepath.Join(os.Getenv("RECOVERY_SMTP_DIR"), "smtp.jsonl"))
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		var r receipt
		if err = json.Unmarshal(scanner.Bytes(), &r); err != nil {
			return err
		}
		address, err := mail.ParseAddress(r.To)
		if err != nil {
			return err
		}
		if r.MessageID != fmt.Sprintf("<gobbs-email-%d@example.test>", id) || address.Address != "reader@example.test" || r.Subject == "" {
			return fmt.Errorf("SMTP receipt mismatch: %+v", r)
		}
		count++
	}
	if err = scanner.Err(); err != nil {
		return err
	}
	if count < 1 || count > attempts || (target == "email" && count != 2) {
		return fmt.Errorf("SMTP receipt count=%d attempts=%d target=%s", count, attempts, target)
	}
	fmt.Printf("SMTP PASS: %d accepted copies, %d job attempts, stable Message-ID\n", count, attempts)
	return nil
}
