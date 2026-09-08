# Usage: jq -s -f scripts/soak-summary.jq /path/to/soak.jsonl
def stats:
  map(select(. != null)) | sort |
  if length == 0 then null
  else {mean: (add / length), p95: .[((length - 1) * 0.95 | floor)], max: .[-1]}
  end;

. as $events |
{
  config: ([$events[] | select(.kind == "config")] | first),
  stages: [$events[] | select(.kind == "stage")],
  samples: ([$events[] | select(.kind == "sample" and .stage > 0)] |
    group_by(.stage) | map(
      . as $rows | .[0] as $first | .[-1] as $last |
      {
        stage: $first.stage,
        samples: length,
        observedSeconds: ($last.elapsedSeconds - $first.elapsedSeconds),
        cpuPercentOneCore: (map(.resources.pgCPUPercentOneCore) | stats),
        physicalReadBytesPerSecond: (map(.resources.pgReadBytesPerSecond) | stats),
        physicalWriteBytesPerSecond: (map(.resources.pgWriteBytesPerSecond) | stats),
        searchPending: (map(.db.search) | stats),
        searchOldestSeconds: (map(.db.searchOldestSeconds) | stats),
        subscriptionsPending: (map(.db.subscriptions) | stats),
        subscriptionOldestSeconds: (map(.db.subscriptionOldestSeconds) | stats),
        forumPending: (map(.db.forum) | stats),
        growthPending: (map(.db.growth) | stats),
        titlesPending: (map(.db.titles) | stats),
        lockWaits: (map(.db.lockWaits) | stats),
        maxWaitingQueryAgeSeconds: (map(.db.maxLockWaitSeconds) | stats),
        canceledAcquires: ($last.pool.canceledAcquireCount - $first.pool.canceledAcquireCount),
        poolMeanAcquireMs: (if $last.pool.acquireCount > $first.pool.acquireCount then
          ($last.pool.acquireDurationMs - $first.pool.acquireDurationMs) /
          ($last.pool.acquireCount - $first.pool.acquireCount) else 0 end),
        deadlocksCumulative: $last.db.database.deadlocks,
        tempBytesCumulative: $last.db.database.temp_bytes,
        pgBufferHits: ($last.db.database.blks_hit - $first.db.database.blks_hit),
        pgBufferReads: ($last.db.database.blks_read - $first.db.database.blks_read),
        endQueues: ($last.db | {search, subscriptions, forum, growth, titles, email})
      })),
  final: ([$events[] | select(.kind == "final")] | last),
  monitorErrors: [$events[] | select(.error != null or .resources.error != null)]
}
