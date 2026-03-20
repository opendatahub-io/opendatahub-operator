# CI Analysis Query Library

BigQuery queries for analyzing OpenShift CI job data for the `opendatahub-io` org. Queries target `openshift-gce-devel.ci_analysis_us.jobs`. ODH prowjob data is visible in [Looker Studio](https://lookerstudio.google.com/u/0/reporting/3d305775-c217-4c12-9331-9768276c3211/page/p_52w2d8a4uc).


## Billing Note

BigQuery charges are based on **bytes scanned per query**, not rows returned. The `openshift-gce-devel.ci_analysis_us.jobs` table is shared infrastructure -- queries against it are billed to whichever GCP project is set as your default (`gcloud config get project`). Each query shows its cost estimate in the BigQuery console before running. Always use `--dry_run` from the CLI to check before executing.

- **On-demand pricing**: $7 per TB scanned beyond free tier
- **Partition filters are critical**: queries without a date filter on the partition column scan the entire table, which can be very expensive
- Narrow the date range as much as possible for your analysis

## Permissions

Access to BigQuery requires membership in the [opendatahub-ci-bigquery](https://rover.redhat.com/groups/group/opendatahub-ci-bigquery) Rover group. To request access, contact one of the group owners or email **opendatahub-ci-bigquery@redhat.com**.

## Location

SQL files are in `hack/ci-analysis/`. Each file is a standalone query that can be run directly in the BigQuery console or via `bq query`.

## Query Catalog

### Flake Detection

| File | Description |
|---|---|
| `flake_rate.sql` | Jobs with mixed pass/fail results, ranked by failure percentage |
| `flaky_prs.sql` | PRs where the same job failed then passed on retry (strongest flake signal) |
| `flake_trend.sql` | Daily failure rate trend across all jobs |
| `retest_triggered.sql` | Jobs with explicit `/retest` commands, ranked by retest count |
| `cluster_flakes.sql` | Failure rates broken down by Prow build cluster |

### Pass Rate

| File | Description |
|---|---|
| `pass_rate_by_job.sql` | Daily pass rate per job |
| `pass_rate_by_type.sql` | Pass rate by job type (presubmit, periodic, postsubmit) |

### Duration

| File | Description |
|---|---|
| `duration_trends.sql` | Daily average duration per job to spot performance regressions |
| `slowest_jobs.sql` | Jobs ranked by average duration |
| `duration_by_status.sql` | Duration comparison between passing and failing runs |
| `pending_time.sql` | Queue wait time by job and cluster |

## CLI Setup

### Prerequisites

Install the Google Cloud SDK (includes `bq` and `gcloud`) by following the official guide: https://cloud.google.com/sdk/docs/install-sdk

Verify the installation:

```bash
gcloud version
bq version
```

### Authentication

```bash
# Login with your Google account (opens browser)
gcloud auth login

# Set the project that hosts the CI data
gcloud config set project openshift-gce-devel
```

If you get a permission error, follow [Permission section](#permissions).

## Usage

### BigQuery Console

Copy the SQL from any file and paste into the [BigQuery console](https://console.cloud.google.com/bigquery?project=openshift-gce-devel). Adjust the date range in the `BETWEEN` clause as needed.

### Dry Run (Check Cost Before Executing)

```bash
bq query --dry_run --use_legacy_sql=false < hack/ci-analysis/flake_rate.sql
```

This shows bytes that would be scanned without actually running the query or incurring cost.

### bq CLI

```bash
# Run a query from file
bq query --use_legacy_sql=false < hack/ci-analysis/flake_rate.sql

# Increase max rows returned (default is 100)
bq query --use_legacy_sql=false --max_rows=1000 < hack/ci-analysis/flake_rate.sql

# Output as CSV
bq query --use_legacy_sql=false --format=csv < hack/ci-analysis/flake_rate.sql > results.csv

# Output as JSON
bq query --use_legacy_sql=false --format=json < hack/ci-analysis/flake_rate.sql

# Save results to a destination table
bq query --use_legacy_sql=false \
  --destination_table=your_project:your_dataset.results_table \
  --replace \
  < hack/ci-analysis/flake_rate.sql
```

## Customizing Date Ranges

All queries default to today's data:

```sql
AND prowjob_start >= DATETIME(CURRENT_DATE()) AND prowjob_start < DATETIME_ADD(CURRENT_DATE(), INTERVAL 1 DAY)
```

Adjust to your needs:
- Last 7 days: `BETWEEN DATETIME_SUB(CURRENT_DATETIME(), INTERVAL 7 DAY) AND CURRENT_DATETIME()`
- Specific date: `BETWEEN DATETIME("2026-03-13") AND DATETIME_ADD("2026-03-13", INTERVAL 1 DAY)`

## Cost Optimization

BigQuery charges by bytes scanned. Key strategies:

1. **`prowjob_start` is the partition column** -- always filter on it to prune partitions
2. **Create a staging table** for repeated analysis to avoid scanning the source multiple times:
   ```sql
   CREATE OR REPLACE TABLE `your-project.your_dataset.odh_ci_jobs` AS
   SELECT created, prowjob_job_name, prowjob_state, prowjob_type,
          prowjob_start, prowjob_completion, prowjob_pending,
          prowjob_build_id, prowjob_cluster, repo, pr_number, retest
   FROM `openshift-gce-devel.ci_analysis_us.jobs`
   WHERE org = 'opendatahub-io'
     AND prowjob_start BETWEEN DATETIME("2026-03-13") AND DATETIME_ADD("2026-03-13", INTERVAL 1 DAY)
     AND prowjob_state IN ('success', 'failure')
   ```
3. **Use `--dry_run`** before executing to check bytes scanned
4. **BigQuery caches** identical queries for 24 hours at no cost
