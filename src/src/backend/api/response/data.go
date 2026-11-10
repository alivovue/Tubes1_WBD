package response

import (
	"time"

	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/database/dao"
)

type RecentExecution struct {
	dao.MigrationExecution
	DatabaseName string
}

type Data struct {
	Title, Active, Section                         string
	User                                           dao.User
	CSRF, Error, Message                           string
	Status                                         int
	Now, Expires                                   time.Time
	Databases                                      []dao.Database
	Database                                       dao.Database
	CanEdit                                        bool
	EditableCount                                  int
	Recent                                         []RecentExecution
	Schemas                                        []dbmanagement.Schema
	Tables                                         []dbmanagement.Table
	Schema, Table, Sort, Direction                 string
	Detail                                         *dbmanagement.TableDetail
	Data                                           *dbmanagement.DataPage
	Offset, Previous, Next                         int
	HasMore                                        bool
	History                                        []dao.MigrationExecution
	Execution                                      *dao.MigrationExecution
	Drafts                                         []dao.MigrationDraft
	DraftID                                        int64
	DraftUpdated, Description, SQL, OperationsJSON string
	Review                                         *migration.Analysis
	SchemaDiff                                     *SchemaDiffView
	ViewerResult                                   *dbmanagement.ReadResult
	ViewerSpec                                     dbmanagement.ReadQuerySpec
	ScrollTarget                                   string
}
