package integration_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd_admin/backend/access"
	"tubes1_wbd_admin/backend/connectionconfig"
	"tubes1_wbd_admin/database/dao"
)

func populateWebsite(ctx context.Context, website *sql.DB, store *dao.Store, auth *access.Service, password string, admin int64) error {
	if err := auth.RequireAdmin(ctx, admin); err != nil {
		return err
	}
	for _, entry := range []struct{ name, description string }{{"Mock Editors", "Sample editors"}, {"Mock Readers", "Sample readers"}} {
		groups, err := store.Groups(ctx)
		if err != nil {
			return err
		}
		exists := false
		for _, group := range groups {
			if group.Name == entry.name {
				exists = true
				break
			}
		}
		if !exists {
			if err := auth.SaveGroup(ctx, admin, 0, entry.name, entry.description); err != nil {
				return err
			}
		}
	}
	if password != "" {
		writer, err := access.HashPassword(password)
		if err != nil {
			return err
		}
		reader, err := access.HashPassword(password)
		if err != nil {
			return err
		}
		accounts, readErr := os.ReadFile(filepath.Join("..", "data", "website", "accounts.sql"))
		if readErr != nil {
			return readErr
		}
		_, err = website.ExecContext(ctx, string(accounts), writer, reader, admin)
		if err != nil {
			return err
		}
		memberships, readErr := os.ReadFile(filepath.Join("..", "data", "website", "memberships.sql"))
		if readErr != nil {
			return readErr
		}
		_, err = website.ExecContext(ctx, string(memberships))
		return err
	}
	return nil
}

// attachManaged seeds only explicitly selected local development fixtures using
// the initializer connection. It never creates an administrator or authenticates
// against the admin database. System-seeded records have no admin attribution.
func attachManaged(ctx context.Context, store *dao.Store, key []byte, input dbmanagement.ConnectionInput) error {
	for _, entry := range []struct{ name, description string }{{"Mock Editors", "Sample editors"}, {"Mock Readers", "Sample readers"}} {
		groups, err := store.Groups(ctx)
		if err != nil {
			return err
		}
		exists := false
		for _, group := range groups {
			if group.Name == entry.name {
				exists = true
			}
		}
		if !exists {
			if err := store.SaveGroup(ctx, 0, entry.name, entry.description); err != nil {
				return err
			}
		}
	}
	users, err := store.Users(ctx)
	if err != nil {
		return err
	}
	connections, err := store.Databases(ctx)
	if err != nil {
		return err
	}
	var target dao.Database
	for _, connection := range connections {
		if connection.DBMS == input.DBMS && connection.Host == input.Host && connection.Port == input.Port && connection.DatabaseName == input.DatabaseName {
			target = connection
			break
		}
	}
	if target.ID == 0 {
		encrypted, encryptErr := connectionconfig.EncryptPassword(key, input.Password)
		if encryptErr != nil {
			return encryptErr
		}
		target = dao.Database{DBMS: input.DBMS, Name: input.Name, Host: input.Host, Port: input.Port, DatabaseName: input.DatabaseName, SSLMode: input.SSLMode, Active: true}
		target.ID, err = store.SaveDatabaseMetadata(ctx, 0, dao.DatabaseMetadata{Database: target, Username: input.Username, EncryptedPassword: encrypted}, time.Now())
		if err != nil {
			return err
		}
	}
	groups, err := store.Groups(ctx)
	if err != nil {
		return err
	}
	memberships, err := store.Memberships(ctx)
	if err != nil {
		return err
	}
	grants, err := store.Grants(ctx)
	if err != nil {
		return err
	}
	for _, group := range groups {
		permission := access.NoAccess
		if group.Name == "Mock Editors" {
			permission = access.ViewEdit
		} else if group.Name == "Mock Readers" {
			permission = access.ViewOnly
		} else {
			continue
		}
		for _, user := range users {
			belongs := group.Name == "Mock Editors" && (user.Username == "mock.writer") || group.Name == "Mock Readers" && user.Username == "mock.reader"
			if !belongs || !user.Active {
				continue
			}
			exists := false
			for _, membership := range memberships {
				if membership.UserID == user.ID && membership.GroupID == group.ID {
					exists = true
					break
				}
			}
			if !exists {
				if err := store.SetMembership(ctx, user.ID, group.ID, true); err != nil {
					return err
				}
			}
		}
		exists := false
		for _, grant := range grants {
			if grant.GroupID == group.ID && grant.DatabaseID == target.ID && grant.Permission == permission {
				exists = true
				break
			}
		}
		if !exists {
			if err := store.SetGrant(ctx, 0, group.ID, target.ID, permission); err != nil {
				return err
			}
		}
	}
	return nil
}
