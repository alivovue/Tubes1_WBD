import { useRef, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { migrationPageSize, migrationPaths, migrationsAPI } from "./api";
import type { JobResponse, JobsResponse } from "./types";
import { Button, Notice, Panel, stamp, Table, usePage } from "./ui";
import { BatchProgress } from "./MigrationBatch";
import {
  MigrationStatus,
  MigrationPagination,
  migrationOffset,
  ResourceState,
  migrationURL,
  useMigrationAction,
  useMigrationResource,
} from "./migrationUI";

const finished = (result: JobResponse) =>
  [
    "succeeded",
    "failed",
    "partial",
    "uncertain",
    "reverted",
    "partially_reverted",
  ].includes(result.job.status) && !result.job.next_retry_at;

export function MigrationJobs() {
  const { data } = usePage();
  const [params] = useSearchParams();
  const offset = migrationOffset(params);
  const resource = useMigrationResource<JobsResponse>(
    migrationPaths.jobs(data.Database.ID, offset),
    5000,
  );
  const selected = params.get("job");
  const jobs = (resource.data?.jobs || []).slice(0, migrationPageSize);
  return (
    <>
      <div className="toolbar">
        <p className="muted">
          Jobs run in the background. You can leave this page while the worker
          runs.
        </p>
        <Button secondary onClick={resource.reload}>
          Refresh activity
        </Button>
      </div>
      {selected && (
        <JobDetail key={selected} id={selected} reloadList={resource.reload} />
      )}
      <Panel
        title="Job activity"
        text="Queued work, failures and retries. Completed attempts are also recorded in migration history."
      >
        <ResourceState {...resource} />
        {resource.data && (
          <>
            <Table
              headers={[
                "Job / migration",
                "Action",
                "Status",
                "Progress",
                "Attempt",
                "Requested",
                "Latest error",
              ]}
              rows={jobs.map((job) => [
                <Link
                  to={migrationURL("jobs", data.Database.ID, {
                    job: job.job_id,
                    offset,
                  })}
                >
                  {job.title}
                  <small className="migration-secondary">{job.job_id}</small>
                </Link>,
                job.operation.replaceAll("_", " "),
                <MigrationStatus status={job.status} />,
                `${job.committed_batches} batches · ${job.committed_rows.toLocaleString()} rows`,
                `${job.attempt} / ${job.max_attempts}`,
                <>
                  {job.requested_by}
                  <small className="migration-secondary">
                    {stamp(job.created_at)}
                  </small>
                </>,
                job.error || job.blocked_reason || "—",
              ])}
              empty={
                offset > 0
                  ? "No jobs on this page. Use Previous to return to newer jobs."
                  : "No migration jobs yet."
              }
            />
            <MigrationPagination
              label="Jobs"
              offset={offset}
              hasMore={resource.data.jobs.length > migrationPageSize}
            />
          </>
        )}
      </Panel>
    </>
  );
}

function JobDetail({ id, reloadList }: { id: string; reloadList: () => void }) {
  const { data } = usePage();
  const navigate = useNavigate();
  const resource = useMigrationResource<JobResponse>(
    migrationPaths.job(id),
    2000,
    finished,
  );
  const action = useMigrationAction();
  const [confirmed, setConfirmed] = useState(false);
  const retryKey = useRef(crypto.randomUUID());
  const job = resource.data?.job;
  async function retry() {
    const result = await action.run((csrf) =>
      migrationsAPI.retry(id, csrf, retryKey.current),
    );
    if (result) {
      retryKey.current = crypto.randomUUID();
      resource.reload();
      reloadList();
    }
  }
  async function requestCancel() {
    if (!confirmed || !job) return;
    const result = await action.run((csrf) =>
      migrationsAPI.cancelReview(job, csrf),
    );
    if (result)
      navigate(
        migrationURL("reviews", data.Database.ID, { review: result.review.id }),
      );
  }
  async function requestResume() {
    if (!job) return;
    const result = await action.run((csrf) => migrationsAPI.resumeReview(job, csrf));
    if (result) navigate(migrationURL("reviews", data.Database.ID, { review: result.review.id }));
  }
  return (
    <Panel title="Job details" text={id}>
      <div className="integration-form">
        <ResourceState {...resource} />
        {job && job.database_id !== data.Database.ID && (
          <Notice error>
            This job does not belong to the selected database.
          </Notice>
        )}
        {job && job.database_id === data.Database.ID && (
          <>
            <div className="migration-meta">
              <h3>{job.title}</h3>
              <MigrationStatus status={job.status} />
              <Button secondary onClick={resource.reload}>
                Refresh job
              </Button>
            </div>
            <dl className="detail-grid">
              <div>
                <dt>Operation</dt>
                <dd>{job.operation.replaceAll("_", " ")}</dd>
              </div>
              <div>
                <dt>Requested by</dt>
                <dd>{job.requested_by}</dd>
              </div>
              <div>
                <dt>Created</dt>
                <dd>{stamp(job.created_at)}</dd>
              </div>
              <div>
                <dt>Started / completed</dt>
                <dd>
                  {stamp(job.started_at || "")} /{" "}
                  {stamp(job.completed_at || "")}
                </dd>
              </div>
              <div>
                <dt>Attempt</dt>
                <dd>
                  {job.attempt} / {job.max_attempts}
                </dd>
              </div>
              <div>
                <dt>Next retry</dt>
                <dd>{stamp(job.next_retry_at || "")}</dd>
              </div>
            </dl>
            {job.operation === "up_batch" ||
            job.operation === "cancel_batch" ||
            job.committed_batches > 0 ? (
              <BatchProgress job={job} />
            ) : null}
            {job.resumes_job_id && <Notice>
              Continues job {job.resumes_job_id} from its saved checkpoint and original row scope.
            </Notice>}
            {job.recovery_job_id && <Link to={migrationURL("jobs", data.Database.ID, { job: job.recovery_job_id })}>
              View recovery job
            </Link>}
            {job.error && <Notice error>{job.error}</Notice>}
            {job.blocked_reason && (
              <Notice error>Database blocked: {job.blocked_reason}</Notice>
            )}
            {action.error && <Notice error>{action.error}</Notice>}
            <div className="heading-actions">
              <Link
                className="button button--secondary"
                to={migrationURL("reviews", data.Database.ID, {
                  review: job.review_id,
                })}
              >
                View approval
              </Link>
              <Link
                className="button button--secondary"
                to={migrationURL("history", data.Database.ID)}
              >
                Migration history
              </Link>
              {data.CanEdit && job.retryable && (
                <Button
                  onClick={retry}
                  disabled={
                    action.busy || !!resource.error || !!job.blocked_reason
                  }
                >
                  Resume / retry from checkpoint
                </Button>
              )}
            </div>
            {data.CanEdit && job.can_resume && <div className="migration-decision form-stack">
              <Notice>
                If approvals or leaders changed, request a fresh review to resume this job.
                Every current group leader must approve the unchanged SQL. The worker continues
                only the uncommitted batches using the original row scope.
              </Notice>
              <Button secondary onClick={requestResume} disabled={action.busy || !!resource.error || !!job.blocked_reason}>
                Request fresh approval to resume
              </Button>
            </div>}
            {data.CanEdit && job.can_cancel && (
              <div className="migration-decision form-stack">
                <Notice>
                  Cancellation requests a new review from every required group
                  leader. After approval, compensation reverses committed
                  batches in reverse order. This does not stop the worker
                  immediately.
                </Notice>
                <label className="check-control">
                  <input
                    type="checkbox"
                    checked={confirmed}
                    disabled={action.busy || !!resource.error}
                    onChange={(event) => setConfirmed(event.target.checked)}
                  />
                  <span>
                    Request approval to compensate this batch using its original
                    reversal plan.
                  </span>
                </label>
                <Button
                  danger
                  onClick={requestCancel}
                  disabled={!confirmed || action.busy || !!resource.error}
                >
                  Request cancellation review
                </Button>
              </div>
            )}
          </>
        )}
      </div>
    </Panel>
  );
}
