/* GoBBS 全站交互：闪现消息、回到顶部、删除确认、Markdown 编辑器 */
(function () {
	'use strict';

	// ---- 闪现消息自动消失 ----
	var flash = document.querySelector('[data-flash]');
	if (flash) {
		setTimeout(function () {
			flash.style.transition = 'opacity .6s';
			flash.style.opacity = '0';
			setTimeout(function () { flash.remove(); }, 650);
		}, 3500);
	}

	// ---- 回到顶部 ----
	var st = document.getElementById('scrolltop');
	if (st) {
		window.addEventListener('scroll', function () {
			st.style.display = window.scrollY > 400 ? 'block' : 'none';
		}, { passive: true });
		st.addEventListener('click', function () {
			window.scrollTo({ top: 0, behavior: 'smooth' });
		});
	}

	// ---- 删除等危险操作确认 ----
	document.addEventListener('submit', function (e) {
		var f = e.target;
		if (f.matches('form[data-confirm]')) {
			if (!window.confirm(f.getAttribute('data-confirm'))) {
				e.preventDefault();
				return;
			}
		}
		// 删除表单的 back 字段：记录当前页地址，删除后跳回
		var back = f.querySelector('input[name="back"]');
		if (back && !back.value) back.value = window.location.pathname;
	}, true);

	// ---- 表格全选（data-checkall）----
	document.addEventListener('change', function (e) {
		var master = e.target.closest('[data-checkall]');
		if (!master) return;
		document.querySelectorAll(master.getAttribute('data-checkall')).forEach(function (c) {
			c.checked = master.checked;
		});
	});

	// ---- 点赞（本地乐观更新 + SSE 全局同步）----
	document.addEventListener('click', function (e) {
		var btn = e.target.closest('.likebtn');
		if (!btn) return;
		if (!document.body.dataset.authed) {
			window.location.href = '/login?next=' + encodeURIComponent(window.location.pathname);
			return;
		}
		var pid = btn.getAttribute('data-pid');
		var csrfEl = document.querySelector('form.inlineform input[name="_csrf"]');
		fetch('/api/like/' + pid, {
			method: 'POST',
			headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
			body: '_csrf=' + encodeURIComponent(csrfEl ? csrfEl.value : '')
		}).then(function (r) { return r.json(); }).then(function (d) {
			if (d.error) return;
			btn.classList.toggle('liked', d.liked);
			var span = document.querySelector('[data-lc="' + pid + '"]');
			if (span) span.textContent = d.count > 0 ? '👍 ×' + d.count : '';
		}).catch(function () { /* 忽略 */ });
	});

	// ---- 点赞名单浮层 ----
	document.addEventListener('click', function (e) {
		var trigger = e.target.closest('[data-likes-toggle]');
		var pop = document.getElementById('likes-pop');
		if (trigger) {
			e.preventDefault();
			var pid = trigger.getAttribute('data-likes-toggle');
			if (pop && pop.dataset.pid === pid) { pop.remove(); return; }
			if (pop) pop.remove();
			fetch('/api/likes/' + pid).then(function (r) { return r.json(); }).then(function (list) {
				if (!list.length) return;
				var box = document.createElement('div');
				box.id = 'likes-pop';
				box.dataset.pid = pid;
				box.className = 'likes-pop';
				list.forEach(function (u) {
					box.insertAdjacentHTML('beforeend',
						'<a class="likes-user" href="/user/' + u.uid + '">' +
						'<img src="/avatar/' + u.uid + '" width="20" height="20" alt="">' + u.name + '</a>');
				});
				document.body.appendChild(box);
				var rect = trigger.getBoundingClientRect();
				box.style.top = (rect.bottom + window.scrollY + 6) + 'px';
				box.style.left = Math.max(8, rect.left + window.scrollX - 8) + 'px';
			}).catch(function () { /* 忽略 */ });
		} else if (pop && !e.target.closest('#likes-pop')) {
			pop.remove();
		}
	});

	// ---- 后台移动端抽屉菜单 ----
	var menuToggle = document.getElementById('admin-menu-toggle');
	if (menuToggle) {
		var backdrop = document.getElementById('admin-menu-backdrop');
		var menuBox = document.getElementById('admin-menu');
		var setMenu = function (open) {
			document.body.classList.toggle('admin-menu-open', open);
			if (backdrop) backdrop.hidden = !open;
			menuToggle.setAttribute('aria-expanded', open ? 'true' : 'false');
		};
		menuToggle.addEventListener('click', function () {
			setMenu(!document.body.classList.contains('admin-menu-open'));
		});
		if (backdrop) backdrop.addEventListener('click', function () { setMenu(false); });
		var closeBtn = document.getElementById('admin-menu-close');
		if (closeBtn) closeBtn.addEventListener('click', function () { setMenu(false); });
		// 点菜单项导航前先收起（防止 bfcache 恢复展开态）
		if (menuBox) menuBox.addEventListener('click', function (e) {
			if (e.target.closest('a')) setMenu(false);
		});
	}

	// ---- Markdown 编辑器 ----
	document.querySelectorAll('form[data-editor]').forEach(initEditor);

	function initEditor(form) {
		var ta = form.querySelector('textarea[name="content"]');
		if (!ta) return;
		var draftKey = 'gobbs_draft:' + (form.getAttribute('action') || 'default');

		// 草稿恢复与自动保存：登录用户云端同步（可跨设备），游客存本机
		var draftCtx = form.getAttribute('data-draft-context');
		var authed = !!document.body.dataset.authed;
		try {
			var saved = localStorage.getItem(draftKey);
			if (saved && !ta.value) ta.value = saved;
			var saveTimer;
			ta.addEventListener('input', function () {
				clearTimeout(saveTimer);
				saveTimer = setTimeout(function () {
					try { localStorage.setItem(draftKey, ta.value); } catch (e) { /* 忽略 */ }
					count();
				}, 400);
			});
		} catch (e) { /* localStorage 不可用时静默降级 */ }
		if (draftCtx && authed) {
			// 恢复云端草稿（本地为空时）
			fetch('/api/draft?context=' + encodeURIComponent(draftCtx))
				.then(function (r) { return r.json(); })
				.then(function (d) {
					if (d.content && !ta.value) { ta.value = d.content; count(); }
				}).catch(function () { /* 忽略 */ });
			// 每 10 秒云端自动保存
			var cloudTimer;
			ta.addEventListener('input', function () {
				clearTimeout(cloudTimer);
				cloudTimer = setTimeout(function () {
					var csrfEl = form.querySelector('input[name="_csrf"]');
						fetch('/api/draft', {
							method: 'POST',
							headers: { 'Content-Type': 'application/json' },
							body: JSON.stringify({ context: draftCtx, content: ta.value, csrf: csrfEl ? csrfEl.value : '' })
						}).catch(function () { /* 忽略 */ });
				}, 10000);
			});
		}

		// 字数统计
		var cc = form.querySelector('[data-charcount]');
		function count() {
			if (cc) cc.textContent = ta.value.length + ' 字';
		}
		count();

		// 提交成功（跳转离开）后清草稿
		form.addEventListener('submit', function () {
			try { localStorage.removeItem(draftKey); } catch (e) { /* 忽略 */ }
			if (draftCtx && authed) {
				var csrfEl2 = form.querySelector('input[name="_csrf"]');
				fetch('/api/draft', {
					method: 'POST', keepalive: true,
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ context: draftCtx, content: '', csrf: csrfEl2 ? csrfEl2.value : '' })
				}).catch(function () { /* 忽略 */ });
			}
		});

		// 工具栏：在选区两侧插入 Markdown 标记
		form.querySelectorAll('.ebtn').forEach(function (btn) {
			btn.addEventListener('click', function () {
				applyAct(btn.getAttribute('data-act'));
				ta.focus();
			});
		});

		function wrap(before, after, placeholder) {
			var s = ta.selectionStart, e = ta.selectionEnd;
			var sel = ta.value.slice(s, e) || placeholder;
			replace(before + sel + after, s, e);
			ta.selectionStart = s + before.length;
			ta.selectionEnd = s + before.length + sel.length;
		}

		function linePrefix(prefix) {
			var s = ta.selectionStart, e = ta.selectionEnd;
			var start = ta.value.lastIndexOf('\n', s - 1) + 1;
			var seg = ta.value.slice(start, e || s);
			var lines = seg.split('\n').map(function (l, i) { return (i === 0 && l === '') ? l : prefix + l; });
			replace(lines.join('\n'), start, e);
		}

		function insert(text) {
			var s = ta.selectionStart, e = ta.selectionEnd;
			replace(text, s, e);
			ta.selectionStart = ta.selectionEnd = s + text.length;
		}

		function replace(text, s, e) {
			ta.value = ta.value.slice(0, s) + text + ta.value.slice(e);
			ta.dispatchEvent(new Event('input', { bubbles: true }));
		}

		function applyAct(act) {
			switch (act) {
				case 'bold': wrap('**', '**', '加粗文本'); break;
				case 'italic': wrap('*', '*', '斜体文本'); break;
				case 'strike': wrap('~~', '~~', '删除线'); break;
				case 'quote': linePrefix('> '); break;
				case 'code': wrap('`', '`', 'code'); break;
				case 'codeblock': wrap('\n```go\n', '\n```\n', '// 代码'); break;
				case 'ul': linePrefix('- '); break;
				case 'ol': linePrefix('1. '); break;
				case 'link': wrap('[', '](https://)', '链接文字'); break;
				case 'img': insert('![图片描述](https://)'); break;
				case 'upload': uploadInput.click(); break;
				case 'table': insert('\n| 列一 | 列二 |\n| --- | --- |\n| 内容 | 内容 |\n'); break;
				case 'hr': insert('\n---\n'); break;
			}
		}

		// 图片/附件上传（按钮选择 + 粘贴/拖入）
		var uploadBtn = form.querySelector('[data-upload-btn]');
		var uploadInput = form.querySelector('[data-upload-input]');
		var fileBtn = form.querySelector('[data-uploadfile-btn]');
		var fileInput = form.querySelector('[data-uploadfile-input]');
		if (uploadBtn && uploadInput) {
			uploadBtn.addEventListener('click', function () { uploadInput.click(); });
			uploadInput.addEventListener('change', function () {
				if (uploadInput.files.length) uploadFile(uploadInput.files[0], 'image');
				uploadInput.value = '';
			});
			ta.addEventListener('paste', function (e) {
				var items = e.clipboardData && e.clipboardData.items;
				if (!items) return;
				for (var i = 0; i < items.length; i++) {
					if (items[i].type.indexOf('image/') === 0) {
						e.preventDefault();
						uploadFile(items[i].getAsFile(), 'image');
						break;
					}
				}
			});
		}
		if (fileBtn && fileInput) {
			fileBtn.addEventListener('click', function () { fileInput.click(); });
			fileInput.addEventListener('change', function () {
				if (fileInput.files.length) uploadFile(fileInput.files[0], 'file');
				fileInput.value = '';
			});
		}
		var uploading = false;
		var maxImageMB = parseInt(form.dataset.maxImageMb || '8', 10);
		var maxFileMB = parseInt(form.dataset.maxFileMb || '20', 10);
		function uploadFile(file, kind) {
			if (!file || uploading) return;
			if (!uploadInput) { alert('本站已切换为仅外链模式，请使用外部链接'); return; }
			var maxSize = kind === 'file' ? maxFileMB : maxImageMB;
			if (file.size > maxSize * 1024 * 1024) { alert('文件不能超过 ' + maxSize + 'MB'); return; }
			uploading = true;
			var fd = new FormData();
			fd.append('file', file);
			fd.append('kind', kind);
			var csrfEl3 = form.querySelector('input[name="_csrf"]');
			fd.append('_csrf', csrfEl3 ? csrfEl3.value : '');
			fetch('/api/upload', { method: 'POST', body: fd })
				.then(function (r) { return r.json(); })
				.then(function (d) {
					uploading = false;
					if (d.error) { alert('上传失败：' + d.error); return; }
					if (d.kind === 'file') insert('\n📎 [' + (d.name || '附件') + '](' + d.url + ')\n');
					else insert('\n![' + (d.name || '图片') + '](' + d.url + ')\n');
				}).catch(function () { uploading = false; });
		}

		// 表情面板
		var smileyBtn = form.querySelector('[data-toggle="#smiley-panel"]');
		var smileyPanel = form.querySelector('#smiley-panel');
		if (smileyBtn && smileyPanel) {
			smileyBtn.addEventListener('click', function () {
				smileyPanel.hidden = !smileyPanel.hidden;
			});
			smileyPanel.addEventListener('click', function (e) {
				var item = e.target.closest('.smiley-item');
				if (!item) return;
				insert(item.getAttribute('data-code') + ' ');
			});
		}

		// 预览（服务端统一渲染，保证与实际发帖一致）
		var previewBtn = form.querySelector('[data-preview]');
		var previewBox = form.querySelector('#preview-box');
		var previewHtml = form.querySelector('[data-preview-html]');
		if (previewBtn && previewBox && previewHtml) {
			var previewing = false;
			previewBtn.addEventListener('click', function () {
				previewBox.hidden = !previewBox.hidden;
				if (!previewBox.hidden) refreshPreview();
			});
			var pvTimer;
			ta.addEventListener('input', function () {
				if (previewBox.hidden) return;
				clearTimeout(pvTimer);
				pvTimer = setTimeout(refreshPreview, 500);
			});
			function refreshPreview() {
				if (previewing) return;
				previewing = true;
				fetch('/api/preview', {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ content: ta.value })
				}).then(function (r) { return r.json(); }).then(function (d) {
					previewHtml.innerHTML = d.html || '';
				}).catch(function () { /* 预览失败静默 */ }).finally(function () {
					previewing = false;
				});
			}
		}
	}
})();
