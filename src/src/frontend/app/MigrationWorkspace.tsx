import { Link, useSearchParams } from "react-router-dom";
import { DatabasePicker, Heading, Notice, url, usePage } from "./ui";
import { MigrationDefinitions } from "./MigrationDefinitions";
import { MigrationReview } from "./MigrationReview";
import { MigrationJobs } from "./MigrationJobs";
import { MigrationHistory } from "./MigrationHistory";
import { migrationURL } from "./migrationUI";

export const migrationViews = [
  "definitions",
  "reviews",
  "jobs",
  "history",
] as const;

export function MigrationWorkspace() {
  const { data } = usePage();
  const [params] = useSearchParams();
  const view = params.get("view") || "definitions";
  return (
    <>
      <Heading
        title="Migration workflow"
        icon="layers"
        text="Draft → Group review → Approved → Queued → Running → Completed"
      >
        <Link
          className="button button--secondary"
          to={url("/migrations/new", { db: data.Database.ID })}
        >
          SQL analysis editor
        </Link>
        <Link
          className="button button--secondary"
          to={url("/migrations", { db: data.Database.ID })}
        >
          Execution attempts
        </Link>
      </Heading>
      <DatabasePicker
        databases={data.Databases}
        selected={data.Database.ID}
        preserve={{ view }}
      />
      {!!data.Database.ID && (
        <nav className="migration-tabs" aria-label="Migration workflow">
          {migrationViews.map((tab) => (
            <Link
              key={tab}
              className={`button button--${tab === view ? "primary" : "secondary"}`}
              aria-current={tab === view ? "page" : undefined}
              to={migrationURL(tab, data.Database.ID)}
            >
              {
                {
                  definitions: "Drafts & SQL",
                  reviews: "Approval inbox",
                  jobs: "Jobs & activity",
                  history: "Committed history",
                }[tab]
              }
            </Link>
          ))}
        </nav>
      )}
      {!data.Database.ID ? (
        <p className="empty-state">
          No database access is assigned. Contact your platform administrator.
        </p>
      ) : (
        <>
          {data.Database.DBMS !== "postgresql" && (
            <Notice>
              Approval and batch workflows support PostgreSQL. Use the SQL
              editor and changelog for MariaDB.
            </Notice>
          )}
          {view === "definitions" && <MigrationDefinitions />}
          {view === "reviews" && <MigrationReview />}
          {view === "jobs" && <MigrationJobs />}
          {view === "history" && <MigrationHistory />}
        </>
      )}
    </>
  );
}
