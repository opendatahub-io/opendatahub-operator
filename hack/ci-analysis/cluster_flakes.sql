-- Cluster-Specific Flakes
-- Identify if flakiness correlates with a specific Prow build cluster.
-- Jobs that fail more on one cluster than another point to infra issues.
SELECT
  prowjob_cluster,
  prowjob_job_name,
  COUNT(*) AS total_runs,
  COUNTIF(prowjob_state = 'failure') AS failures,
  ROUND(COUNTIF(prowjob_state = 'failure') / COUNT(*) * 100, 2) AS failure_pct
FROM `openshift-gce-devel.ci_analysis_us.jobs`
WHERE prowjob_state IN ('success', 'failure')
  AND org = 'opendatahub-io'
  AND prowjob_start >= DATETIME(CURRENT_DATE()) AND prowjob_start < DATETIME_ADD(CURRENT_DATE(), INTERVAL 1 DAY)
GROUP BY prowjob_cluster, prowjob_job_name
HAVING total_runs >= 5 AND failures > 0
ORDER BY failure_pct DESC
LIMIT 1000