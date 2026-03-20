-- Flake Trend Over Time (Daily)
-- Track whether flakiness is improving or worsening.
-- Widen the date range (e.g., INTERVAL 30 DAY) for a meaningful trend.
SELECT
  DATE(created) AS day,
  COUNT(*) AS total_runs,
  COUNTIF(prowjob_state = 'failure') AS failures,
  ROUND(COUNTIF(prowjob_state = 'failure') / COUNT(*) * 100, 2) AS failure_pct
FROM `openshift-gce-devel.ci_analysis_us.jobs`
WHERE prowjob_state IN ('success', 'failure')
  AND org = 'opendatahub-io'
  AND prowjob_start >= DATETIME(CURRENT_DATE()) AND prowjob_start < DATETIME_ADD(CURRENT_DATE(), INTERVAL 1 DAY)
GROUP BY day
ORDER BY day
LIMIT 1000