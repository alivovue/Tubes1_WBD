import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import type { Database } from "./types";
import {
  Button,
  Check,
  Field,
  Form,
  Heading,
  Hidden,
  initials,
  Notice,
  Panel,
  permission,
  Table,
  usePage,
} from "./ui";

export function Users({
  administrators = false,
}: {
  administrators?: boolean;
}) {
  const { data } = usePage();
  const label = administrators ? "administrator" : "employee";
  const accounts = administrators ? data.Administrators : data.Admin.Users;
  return (
    <>
      <Heading
        title={administrators ? "Administrators" : "Employees"}
        text={
          administrators
            ? "Kelola akun admin dan sesi dalam database autentikasi admin."
            : "Kelola akun dan status karyawan. Akun ini hanya untuk aplikasi pengguna."
        }
      >
        <Link className="button button--secondary" to="/admin/groups">
          Manage memberships
        </Link>
      </Heading>
      <Panel
        title={`Add ${label}`}
        text="Passwords must contain 12–72 bytes. Password changes and deactivation revoke sessions. Admin and ordinary accounts have independent credentials and permissions."
      >
        <Form>
          <Hidden
            name="action"
            value={administrators ? "create_admin" : "create_user"}
          />
          <div className="form-grid">
            <Field label="Username">
              <input
                name="username"
                required
                minLength={3}
                maxLength={100}
                autoComplete="off"
              />
            </Field>
            <Field label="Display name">
              <input name="display" required maxLength={150} />
            </Field>
            <Field label="Initial password">
              <input
                name="password"
                type="password"
                required
                minLength={12}
                maxLength={72}
                autoComplete="new-password"
              />
            </Field>
          </div>
          <Button>Create {label}</Button>
        </Form>
      </Panel>
      <Panel>
        <Table
          headers={[
            administrators ? "Administrator" : "Employee",
            "Username",
            "Role",
            "Status",
            "Edit",
          ]}
          rows={(accounts || []).map((user) => [
            user.DisplayName,
            <code>{user.Username}</code>,
            administrators ? "Platform Admin" : "Employee",
            user.Active ? "Active" : "Disabled",
            <details>
              <summary>Edit account</summary>
              <Form className="form-stack inline-editor">
                <Hidden
                  name="action"
                  value={administrators ? "update_admin" : "update_user"}
                />
                <Hidden name="id" value={user.ID} />
                <Field label="Display name">
                  <input
                    name="display"
                    defaultValue={user.DisplayName}
                    required
                    maxLength={150}
                  />
                </Field>
                <Field label="New password (leave blank to keep)">
                  <input
                    type="password"
                    name="password"
                    minLength={12}
                    maxLength={72}
                    autoComplete="new-password"
                  />
                </Field>
                <Check name="active" checked={user.Active}>
                  Active
                </Check>
                <Button>Save account</Button>
              </Form>
            </details>,
          ])}
          empty={`No ${label} accounts.`}
        />
      </Panel>
    </>
  );
}

export function Groups() {
  const { data } = usePage();
  return (
    <>
      <Heading
        title="Groups"
        text="Kelola grup, anggota, dan akses database dalam satu halaman."
      >
        <Link className="button button--secondary" to="/admin/connections">
          Manage database connections
        </Link>
      </Heading>
      <Notice>
        <strong>Highest permission wins</strong>
        <p>
          No Access &lt; View Only &lt; View + Edit. Employees can join multiple
          groups. Platform administrators manage access only. Group membership
          never grants administrators access to managed data. Reducing or
          removing access requires affected users to sign in again.
        </p>
      </Notice>
      <Panel title="Create group">
        <Form>
          <Hidden name="action" value="save_group" />
          <div className="form-grid">
            <Field label="Name">
              <input name="name" required maxLength={100} />
            </Field>
            <Field label="Description">
              <input name="description" maxLength={2000} />
            </Field>
          </div>
          <Button>Create group</Button>
        </Form>
      </Panel>
      <section
        className="group-grid group-grid--access"
        aria-label="Groups and database permissions"
      >
        {data.Groups?.map((group) => (
          <GroupCard key={group.ID} group={group} />
        ))}
        {!data.Groups?.length && (
          <p className="empty-state">
            Create a group to start assigning database access.
          </p>
        )}
      </section>
    </>
  );
}

function GroupCard({
  group,
}: {
  group: NonNullable<ReturnType<typeof usePage>["data"]["Groups"]>[number];
}) {
  const { data } = usePage();
  const [editor, setEditor] = useState(group.Editor);
  useEffect(() => {
    setEditor(group.Editor);
  }, [group.Editor]);
  useEffect(() => {
    if (group.Focus)
      document
        .getElementById(`group-${group.ID}`)
        ?.scrollIntoView({ behavior: "smooth", block: "start" });
  }, [group.Focus, group.ID]);
  const heading = (title: string) => (
    <div className="group-editor__heading">
      <h3>{title}</h3>
      <Button secondary onClick={() => setEditor("")}>
        Close
      </Button>
    </div>
  );
  return (
    <article
      className="group-card workflow-scroll-target"
      id={`group-${group.ID}`}
      tabIndex={-1}
    >
      <header>
        <div>
          <h2>{group.Name}</h2>
          <p>{group.Description || "No description."}</p>
        </div>
        <span className="badge badge--neutral">
          {group.Members?.length || 0} members
        </span>
      </header>
      <div className="group-card__body">
        <section className="group-members" aria-label={`${group.Name} members`}>
          <h3>Members</h3>
          <ul className="member-list">
            {group.Members?.map((user) => (
              <li key={user.ID}>
                {user.DisplayName} <small>({user.Username})</small>
              </li>
            ))}
            {!group.Members?.length && <li>No members yet.</li>}
          </ul>
        </section>
        <section
          className="group-access"
          aria-label={`${group.Name} database access summary`}
        >
          <h3>Database access</h3>
          <Table
            headers={["Database", "Permission"]}
            rows={(group.Grants || []).map((grant) => [
              <>
                <strong>{grant.Database.Name}</strong>
                <small className="group-access__database-type">
                  {grant.Database.DBMS}
                  {!grant.Database.Active && " · Disabled"}
                </small>
              </>,
              <span
                className={`badge ${grant.Permission === "view_edit" ? "group-permission--edit" : grant.Permission === "view_only" ? "group-permission--view" : "badge--neutral"}`}
              >
                {permission(grant.Permission)}
              </span>,
            ])}
            empty="No database connections registered."
          />
        </section>
      </div>
      <div
        className="group-card__actions"
        role="group"
        aria-label={`Edit ${group.Name}`}
      >
        {[
          ["membership", "Edit membership"],
          ["access", "Edit access"],
          ["information", "Edit group"],
        ].map(([key, label]) => (
          <button
            type="button"
            key={key}
            className="button button--secondary button--small"
            aria-expanded={editor === key}
            aria-controls={`group-${key}-${group.ID}`}
            onClick={() => setEditor((current) => (current === key ? "" : key))}
          >
            {label}
          </button>
        ))}
      </div>
      <section
        className="group-editor"
        id={`group-membership-${group.ID}`}
        hidden={editor !== "membership"}
      >
        {heading("Edit membership")}
        <Form>
          <Hidden name="action" value="membership" />
          <Hidden name="id" value={group.ID} />
          <Field label="Employee">
            <select name="user" required>
              {data.Admin.Users?.map((user) => (
                <option key={user.ID} value={user.ID}>
                  {user.DisplayName} ({user.Username})
                </option>
              ))}
            </select>
          </Field>
          <Field label="Membership">
            <select name="member">
              <option value="yes">Add to group</option>
              <option value="no">Remove from group</option>
            </select>
          </Field>
          <Button secondary>Update membership</Button>
        </Form>
      </section>
      <section
        className="group-editor group-access"
        id={`group-access-${group.ID}`}
        hidden={editor !== "access"}
      >
        {heading("Edit database access")}
        <p className="muted">
          Assign a permission for each database, then save all changes together.
        </p>
        <Form>
          <Hidden name="action" value="set_grants" />
          <Hidden name="id" value={group.ID} />
          <Table
            headers={["Database", "Permission"]}
            rows={(group.Grants || []).map((grant) => [
              <>
                <strong>{grant.Database.Name}</strong>
                <small className="group-access__database-type">
                  {grant.Database.DBMS}
                </small>
              </>,
              <>
                <Hidden name="db" value={grant.Database.ID} />
                <select
                  className="permission-select"
                  name="permission"
                  defaultValue={grant.Permission}
                  aria-label={`${group.Name} ${grant.Database.Name}`}
                >
                  <option value="">No Access</option>
                  <option value="view_only">View Only</option>
                  <option value="view_edit">View + Edit</option>
                </select>
              </>,
            ])}
            empty="No database connections registered."
          />
          {!!group.Grants?.length && <Button>Save access</Button>}
        </Form>
      </section>
      <section
        className="group-editor"
        id={`group-information-${group.ID}`}
        hidden={editor !== "information"}
      >
        {heading("Edit group information")}
        <Form>
          <Hidden name="action" value="save_group" />
          <Hidden name="id" value={group.ID} />
          <Field label="Name">
            <input
              name="name"
              defaultValue={group.Name}
              required
              maxLength={100}
            />
          </Field>
          <Field label="Description">
            <input
              name="description"
              defaultValue={group.Description}
              maxLength={2000}
            />
          </Field>
          <Button>Save group</Button>
        </Form>
        <div className="group-editor__delete">
          <Form confirm="Delete this group and its memberships and database grants?">
            <Hidden name="action" value="delete_group" />
            <Hidden name="id" value={group.ID} />
            <Button danger>Delete group</Button>
          </Form>
        </div>
      </section>
    </article>
  );
}

export function Connections() {
  const { data } = usePage();
  return (
    <>
      <Heading
        title="Database Connections"
        text="Daftarkan target PostgreSQL atau MariaDB. Konfigurasi disimpan untuk aplikasi pengguna."
      />
      <Panel
        title="Add connection"
        text="Credentials are encrypted in the administration database."
      >
        <ConnectionForm />
      </Panel>
      <section className="connection-list">
        {data.Admin.Databases?.map((database) => (
          <article
            className="connection-card integration-connection"
            key={database.ID}
          >
            <div className="connection-card__main">
              <span className="db-mark db-mark--blue">
                {initials(database.Name)}
              </span>
              <div>
                <h2>{database.Name}</h2>
                <p>
                  {database.DBMS} · {database.Host}:{database.Port} /{" "}
                  {database.DatabaseName}
                </p>
                <span className="badge badge--neutral">
                  {database.Active ? "Active" : "Disabled"}
                </span>
              </div>
            </div>
            <details>
              <summary>Connection settings</summary>
              <ConnectionForm database={database} />
            </details>
            {database.Active && (
              <Form confirm="Disable this connection?">
                <Hidden name="id" value={database.ID} />
                <Button danger action="disable_connection">
                  Disable connection
                </Button>
              </Form>
            )}
          </article>
        ))}
        {!data.Admin.Databases?.length && (
          <p className="empty-state">No connections registered.</p>
        )}
      </section>
    </>
  );
}

function ConnectionForm({ database }: { database?: Database }) {
  const { data } = usePage();
  return (
    <Form>
      <Hidden name="id" value={database?.ID || ""} />
      {database && <Hidden name="dbms" value={database.DBMS} />}
      <div className="form-grid">
        <Field label="Display name">
          <input
            name="name"
            defaultValue={database?.Name}
            required
            maxLength={150}
          />
        </Field>
        <Field label="DBMS">
          <select name="dbms" defaultValue={database?.DBMS || "postgresql"} disabled={!!database}>
            {data.DBMS?.map((value) => (
              <option key={value}>{value}</option>
            ))}
          </select>
        </Field>
        <Field label="Host">
          <input
            name="host"
            defaultValue={database?.Host}
            readOnly={!!database}
            required
            autoComplete="off"
          />
        </Field>
        <Field label="Port (blank uses DBMS default)">
          <input
            name="port"
            type="number"
            min="1"
            max="65535"
            defaultValue={database?.Port}
            readOnly={!!database}
          />
        </Field>
        <Field label="Database name">
          <input
            name="database_name"
            defaultValue={database?.DatabaseName}
            readOnly={!!database}
            required
          />
        </Field>
        <Field label={database ? "Username (blank keeps current)" : "Username"}>
          <input name="username" required={!database} autoComplete="off" />
        </Field>
        <Field label={database ? "Password (blank keeps current)" : "Password"}>
          <input type="password" name="password" autoComplete="new-password" />
        </Field>
        <Field label="TLS">
          <select
            name="sslmode"
            defaultValue={database?.SSLMode || "verify-full"}
          >
            <option value="verify-full">Verify server certificate</option>
            <option value="require">Require TLS</option>
            <option value="disable">Disable (local development)</option>
          </select>
        </Field>
      </div>
      <Check name="active" checked={database?.Active ?? true}>
        Active
      </Check>
      <div className="heading-actions">
        <Button action="save_connection">
          {database ? "Save connection" : "Save connection"}
        </Button>
      </div>
    </Form>
  );
}
