import { useCallback, useRef, useState, type FormEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import {
  loadReviewInbox,
  migrationPageSize,
  migrationPaths,
  migrationsAPI,
} from "./api";
import type {
  MigrationReview as ReviewDTO,
  RequiredApproval,
  ReviewsResponse,
  ReviewResponse,
} from "./types";
import {
  Button,
  Field,
  Notice,
  Panel,
  permission,
  stamp,
  Table,
  usePage,
} from "./ui";
import {
  MigrationStatus,
  MigrationPagination,
  migrationOffset,
  ResourceState,
  SQLDetails,
  migrationURL,
  useMigrationAction,
  useMigrationResource,
} from "./migrationUI";

export function MigrationReview() {
  const [params] = useSearchParams();
  const selected = params.get("review");
  return selected ? (
    <ReviewDetail key={selected} id={selected} />
  ) : (
    <ReviewList />
  );
}

function ReviewDetail({ id }: { id: string }) {
  const { data } = usePage();
  const resource = useMigrationResource<ReviewResponse>(
    migrationPaths.review(id),
    5000,
  );
  const review = resource.data?.review;
  return (
    <>
      <div className="toolbar">
        <Link to={migrationURL("reviews", data.Database.ID)}>
          Show all reviews
        </Link>
        <Button secondary onClick={resource.reload}>
          Refresh review
        </Button>
      </div>
      <ResourceState {...resource} />
      {review &&
        (review.database_id === data.Database.ID ? (
          <ReviewCard
            key={review.id}
            review={review}
            reload={resource.reload}
            stale={!!resource.error}
          />
        ) : (
          <Notice error>
            This review does not belong to the selected database.
          </Notice>
        ))}
    </>
  );
}

function ReviewList() {
  const { data } = usePage();
  const [params, setParams] = useSearchParams();
  const offset = migrationOffset(params);
  const inbox = params.get("inbox") === "true";
  const loadInbox = useCallback(
    (signal: AbortSignal) => loadReviewInbox(data.Database.ID, offset, signal),
    [data.Database.ID, offset],
  );
  const resource = useMigrationResource<ReviewsResponse>(
    migrationPaths.reviews(data.Database.ID, offset),
    5000,
    undefined,
    inbox ? loadInbox : undefined,
  );
  const reviews = (resource.data?.reviews || []).slice(0, migrationPageSize);
  return (
    <>
      <div className="toolbar">
        <label className="check-control">
          <input
            type="checkbox"
            checked={inbox}
            onChange={(event) => {
              const next = new URLSearchParams(params);
              if (event.target.checked) next.set("inbox", "true");
              else next.delete("inbox");
              next.delete("offset");
              setParams(next);
            }}
          />
          <span>Only reviews awaiting my decision</span>
        </label>
        <Button secondary onClick={resource.reload}>
          Refresh reviews
        </Button>
      </div>
      <ResourceState {...resource} />
      {reviews.map((review) => (
        <ReviewCard
          key={review.id}
          review={review}
          reload={resource.reload}
          stale={!!resource.error}
        />
      ))}
      {resource.data && !reviews.length && (
        <p className="empty-state">
          {offset > 0
            ? "No reviews on this page. Use Previous to return to newer reviews."
            : inbox
              ? "No reviews currently need your decision."
              : "No submitted reviews for this database."}
        </p>
      )}
      {resource.data && (
        <MigrationPagination
          offset={offset}
          hasMore={resource.data.reviews.length > migrationPageSize}
          label="Reviews"
        />
      )}
    </>
  );
}

function ReviewCard({
  review,
  reload,
  stale,
}: {
  review: ReviewDTO;
  reload: () => void;
  stale: boolean;
}) {
  const { data } = usePage();
  const navigate = useNavigate();
  const action = useMigrationAction();
  const [confirmed, setConfirmed] = useState(false);
  // Keep the same key across uncertain network responses and manual retries.
  const enqueueKey = useRef(crypto.randomUUID());
  const approved = review.required_groups.filter(
    (group) => group.decision === "approved",
  ).length;
  const allApproved =
    review.required_groups.length > 0 &&
    review.required_groups.every(
      (group) => group.leader_user_id && group.decision === "approved",
    );
  async function enqueue() {
    if (!confirmed || !allApproved) return;
    const result = await action.run((csrf) =>
      migrationsAPI.enqueue(review.id, csrf, enqueueKey.current),
    );
    if (result)
      navigate(migrationURL("jobs", data.Database.ID, { job: result.job_id }));
  }
  async function resubmit() {
    const result = await action.run((csrf) =>
      migrationsAPI.resubmit(review, csrf),
    );
    if (result) {
      reload();
      navigate(
        migrationURL("reviews", data.Database.ID, { review: result.review.id }),
      );
    }
  }
  return (
    <Panel
      title={`Review #${review.id} · ${review.title}`}
      text={`${review.action.replaceAll("_", " ")} · Revision ${review.revision} · Requested by ${review.requested_by} on ${stamp(review.requested_at)}`}
    >
      <div className="integration-form migration-review-card">
        <div className="migration-meta">
          <MigrationStatus status={review.status} />
          <strong>
            {approved} / {review.required_groups.length} group approvals
          </strong>
        </div>
        {review.reason && <Notice error>{review.reason}</Notice>}
        {review.status === "invalidated" && (
          <Notice error>
            Authority or migration content changed. Request a new review before
            queueing.
          </Notice>
        )}
        {review.status === "rejected" && (
          <Notice error>
            This review was rejected. Revise the draft and start a new review
            cycle.
          </Notice>
        )}
        {review.resumes_job_id && <Notice>
          Resume job {review.resumes_job_id} from its saved checkpoint. Committed batches
          remain applied and will not run again. This review authorizes the original SQL and row scope.
        </Notice>}
        {review.target_execution_id && (
          <p>Exact target execution: #{review.target_execution_id}</p>
        )}
        <SQLDetails
          up={review.up_sql}
          down={review.down_sql}
          irreversible={review.irreversible}
          reason={review.irreversibility_reason}
        />
        {review.batch && (
          <p>
            Batch target:{" "}
            <code>
              {review.batch.schema}.{review.batch.table}
            </code>{" "}
            · Key: <code>{review.batch.key_column}</code> ·{" "}
            {review.batch.batch_size.toLocaleString()} rows per batch
          </p>
        )}
        <p className="muted">
          Leaders of every access group must approve, including View Only
          groups. Each decision applies to this revision and action.
        </p>
        {!review.required_groups.length && (
          <Notice error>
            No required leaders were resolved. This review cannot be queued.
          </Notice>
        )}
        {review.required_groups.some((group) => !group.leader_user_id) && (
          <Notice>
            This review stays pending while a group has no active leader. Ask a
            platform administrator to assign one. Changed authority requires a
            fresh review before execution.
          </Notice>
        )}
        <Table
          headers={[
            "Group / access",
            "Required leader",
            "Decision",
            "Reason / date",
          ]}
          rows={review.required_groups.map((group) => [
            <>
              {group.group_name}
              <small className="migration-secondary">
                {permission(group.permission)}
              </small>
            </>,
            group.leader_name || (
              <span className="migration-warning">Missing active leader</span>
            ),
            <MigrationStatus status={group.decision} />,
            <>
              {group.reason || "—"}
              <small className="migration-secondary">
                {group.decided_by} {group.decided_at && stamp(group.decided_at)}
              </small>
            </>,
          ])}
        />
        {review.status === "pending_review" &&
          review.required_groups
            .filter((group) => group.can_decide && group.decision === "pending")
            .map((group) => (
              <DecisionForm
                key={group.group_id}
                review={review.id}
                group={group}
                reload={reload}
                stale={stale}
              />
            ))}
        {action.error && <Notice error>{action.error}</Notice>}
        {data.CanEdit &&
          review.can_resubmit &&
          ["rejected", "invalidated"].includes(review.status) && (
            <div className="heading-actions">
              <Button
                secondary
                onClick={resubmit}
                disabled={stale || action.busy}
              >
                Request fresh review
              </Button>
              <Link
                className="button button--secondary"
                to={migrationURL("definitions", data.Database.ID, {
                  copy: review.definition_id,
                })}
              >
                Revise as a new draft
              </Link>
            </div>
          )}
        {review.job_id ? (
          <Link
            className="button button--secondary"
            to={migrationURL("jobs", data.Database.ID, { job: review.job_id })}
          >
            View queued job
          </Link>
        ) : (
          review.status === "approved" &&
          review.can_enqueue &&
          data.CanEdit && (
            <>
              <label className="check-control">
                <input
                  type="checkbox"
                  disabled={stale || action.busy || !allApproved}
                  checked={confirmed}
                  onChange={(event) => setConfirmed(event.target.checked)}
                />
                <span>
                  Queue the approved {review.action.replaceAll("_", " ")} on{" "}
                  {data.Database.Name}. The worker will revalidate authority
                  before execution.
                </span>
              </label>
              <Button
                onClick={enqueue}
                disabled={stale || action.busy || !confirmed || !allApproved}
              >
                Queue approved migration
              </Button>
            </>
          )
        )}
        <details className="migration-audit">
          <summary>Approval identity</summary>
          <p>
            Definition hash: <code>{review.definition_hash}</code>
          </p>
          <p>
            Authority revision: <code>{review.authority_revision}</code>
          </p>
        </details>
      </div>
    </Panel>
  );
}

function DecisionForm({
  review,
  group,
  reload,
  stale,
}: {
  review: number;
  group: RequiredApproval;
  reload: () => void;
  stale: boolean;
}) {
  const [decision, setDecision] = useState<"approved" | "rejected">("approved");
  const [reason, setReason] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const action = useMigrationAction();
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!confirmed || !group.leader_user_id) return;
    const result = await action.run((csrf) =>
      migrationsAPI.decide(
        review,
        group.group_id,
        decision,
        reason.trim(),
        csrf,
      ),
    );
    if (result) reload();
  }
  return (
    <form className="migration-decision form-stack" onSubmit={submit}>
      <h3>Your decision for {group.group_name}</h3>
      {action.error && <Notice error>{action.error}</Notice>}
      <fieldset
        className="form-fields"
        disabled={stale || action.busy || !group.leader_user_id}
      >
        <Field label={`Decision for ${group.group_name}`}>
          <select
            value={decision}
            onChange={(event) => {
              setDecision(event.target.value as typeof decision);
              setConfirmed(false);
            }}
          >
            <option value="approved">Approve</option>
            <option value="rejected">Reject</option>
          </select>
        </Field>
        <Field
          label={`Reason for ${group.group_name}${decision === "rejected" ? " (required)" : " (optional)"}`}
        >
          <textarea
            rows={2}
            maxLength={2000}
            required={decision === "rejected"}
            value={reason}
            onChange={(event) => setReason(event.target.value)}
          />
        </Field>
        <label className="check-control">
          <input
            type="checkbox"
            required
            checked={confirmed}
            onChange={(event) => setConfirmed(event.target.checked)}
          />
          <span>
            I reviewed this action and its immutable SQL as leader of{" "}
            {group.group_name}.
          </span>
        </label>
        <Button
          disabled={
            !confirmed ||
            action.busy ||
            (decision === "rejected" && !reason.trim())
          }
          danger={decision === "rejected"}
        >
          Record {decision === "approved" ? "approval" : "rejection"} for{" "}
          {group.group_name}
        </Button>
      </fieldset>
    </form>
  );
}
