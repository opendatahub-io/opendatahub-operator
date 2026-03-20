-- Flaky PRs: Same PR + Job Failed Then Passed
-- Most reliable flake signal: same job on the same PR fails then succeeds on retry.
-- Eliminates legitimate code bugs (which fail consistently).
WITH pr_job_runs AS (
  SELECT
    repo, pr_number, prowjob_job_name,
    prowjob_build_id, prowjob_state, created,
    ROW_NUMBER() OVER (
      PARTITION BY repo, pr_number, prowjob_job_name
      ORDER BY created, prowjob_build_id
    ) AS run_order
  FROM `openshift-gce-devel.ci_analysis_us.jobs`
  WHERE prowjob_type = 'presubmit'
    AND org = 'opendatahub-io'
    AND pr_number IS NOT NULL
    AND prowjob_state IN ('success', 'failure')
    AND prowjob_start >= DATETIME(CURRENT_DATE()) AND prowjob_start < DATETIME_ADD(CURRENT_DATE(), INTERVAL 1 DAY)
)
SELECT
  a.repo, a.pr_number, a.prowjob_job_name,
  a.created AS failed_at,
  b.created AS passed_at
FROM pr_job_runs a
JOIN pr_job_runs b
  ON a.repo = b.repo
  AND a.pr_number = b.pr_number
  AND a.prowjob_job_name = b.prowjob_job_name
  AND b.run_order = a.run_order + 1
WHERE a.prowjob_state = 'failure'
  AND b.prowjob_state = 'success'
ORDER BY a.created DESC
LIMIT 1000