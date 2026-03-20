-- Retest-Triggered Runs
-- High retest rates indicate flakiness. The retest column flags explicit
-- /retest commands from PR authors.
SELECT
  prowjob_job_name,
  repo,
  COUNTIF(retest IS NOT NULL AND retest = TRUE) AS retest_count,
  COUNT(*) AS total_runs,
  ROUND(COUNTIF(retest IS NOT NULL AND retest = TRUE) / COUNT(*) * 100, 2) AS retest_pct
FROM `openshift-gce-devel.ci_analysis_us.jobs`
WHERE prowjob_type = 'presubmit'
  AND org = 'opendatahub-io'
  AND prowjob_state IN ('success', 'failure')
  AND prowjob_start >= DATETIME(CURRENT_DATE()) AND prowjob_start < DATETIME_ADD(CURRENT_DATE(), INTERVAL 1 DAY)
GROUP BY prowjob_job_name, repo
HAVING retest_count > 0
ORDER BY retest_count DESC
LIMIT 1000