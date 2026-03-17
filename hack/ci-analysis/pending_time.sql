-- Pending Time Analysis
-- Time spent waiting in queue before execution starts.
-- High pending time may indicate resource starvation on a cluster.
SELECT
  prowjob_job_name,
  prowjob_cluster,
  COUNT(*) AS total_runs,
  ROUND(AVG(DATETIME_DIFF(prowjob_start, prowjob_pending, SECOND)) / 60, 2) AS avg_pending_min,
  ROUND(MAX(DATETIME_DIFF(prowjob_start, prowjob_pending, SECOND)) / 60, 2) AS max_pending_min
FROM `openshift-gce-devel.ci_analysis_us.jobs`
WHERE prowjob_state IN ('success', 'failure')
  AND org = 'opendatahub-io'
  AND prowjob_start IS NOT NULL
  AND prowjob_pending IS NOT NULL
  AND prowjob_start >= DATETIME(CURRENT_DATE()) AND prowjob_start < DATETIME_ADD(CURRENT_DATE(), INTERVAL 1 DAY)
GROUP BY prowjob_job_name, prowjob_cluster
ORDER BY avg_pending_min DESC
LIMIT 1000