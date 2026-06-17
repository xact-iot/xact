// Package db defines the database interface and data types for persistent storage.
package sqldb

import (
	"context"
	"encoding/json"
	"time"

	"github.com/xact-iot/xact/backups"
	"github.com/xact-iot/xact/events"
)

// DB defines the interface for all persistent database operations.
// All implementations must be safe for concurrent use.
type DB interface {
	// ListDashboards returns dashboard metadata (without widgets) for an organisation.
	ListDashboards(ctx context.Context, org string) ([]DashboardMeta, error)

	// GetDashboard returns a single dashboard with full data including widgets.
	GetDashboard(ctx context.Context, org string, id int) (*Dashboard, error)

	// CreateDashboard creates a new dashboard for the given organisation.
	CreateDashboard(ctx context.Context, org string, dashboard *Dashboard) error

	// UpdateDashboard updates an existing dashboard identified by organisation and id.
	UpdateDashboard(ctx context.Context, org string, id int, dashboard *Dashboard) error

	// DeleteDashboard removes a dashboard identified by organisation and id.
	DeleteDashboard(ctx context.Context, org string, id int) error

	// ListPermissions returns all role permission records for an organisation.
	ListPermissions(ctx context.Context, org string) ([]RolePermissions, error)

	// GetPermissions returns the permission record for a specific role in an organisation.
	GetPermissions(ctx context.Context, org string, role string) (*RolePermissions, error)

	// UpdatePermissions updates the permission record for a specific role.
	UpdatePermissions(ctx context.Context, org string, role string, perms *RolePermissions) error

	// SaveConfig upserts a named config blob for an organisation.
	SaveConfig(ctx context.Context, org string, configName string, config json.RawMessage) error

	// LoadConfig retrieves the latest config blob by org and name. Returns nil if not found.
	LoadConfig(ctx context.Context, org string, configName string) (json.RawMessage, error)

	// InsertEventEntries batch-inserts event entries.
	InsertEventEntries(ctx context.Context, entries []events.EventEntry) error

	// QueryEvents returns event entries matching the filter.
	QueryEvents(ctx context.Context, filter EventFilter) ([]events.EventEntry, error)

	// PurgeEventsBefore deletes event entries older than the given time.
	PurgeEventsBefore(ctx context.Context, before time.Time) error

	// Migrate applies database migrations. Safe to call on every startup.
	Migrate(ctx context.Context) error

	// Close releases database resources.
	Close()

	// ListRoles returns all defined roles.
	ListRoles(ctx context.Context) ([]Role, error)

	// ListUsers returns all users (without password hashes).
	ListUsers(ctx context.Context) ([]User, error)

	// GetUserByID returns a user by ID with their org/role memberships. Returns nil if not found.
	GetUserByID(ctx context.Context, id int) (*User, error)

	// GetUserByLogin finds a user by login name or email.
	// Returns (user, passwordHash, error). Returns nil user if not found.
	GetUserByLogin(ctx context.Context, login string) (*User, string, error)

	// CreateUser creates a new user. Sets user.ID on success.
	// passwordHash is the bcrypt-hashed password.
	CreateUser(ctx context.Context, user *User, passwordHash string) error

	// UpdateUser updates user fields (not password).
	UpdateUser(ctx context.Context, user *User) error

	// SetUserPassword updates a user's password hash.
	SetUserPassword(ctx context.Context, id int, passwordHash string) error

	// GetUserAuthState returns active/session state used to validate JWTs.
	GetUserAuthState(ctx context.Context, id int) (active bool, tokenVersion int, err error)

	// BumpUserTokenVersion invalidates existing JWTs for a user.
	BumpUserTokenVersion(ctx context.Context, id int) error

	// UpdateLastLogin records the current time as last_login for the user.
	UpdateLastLogin(ctx context.Context, id int) error

	// GetUserOrgs returns all organisations a user belongs to, with their role names.
	GetUserOrgs(ctx context.Context, userID int) ([]UserOrg, error)

	// AddUserToOrg adds a user to an organisation (membership only, no roles).
	AddUserToOrg(ctx context.Context, userID, orgID int) error

	// AssignRoleToUser assigns a named role to a user within an organisation.
	AssignRoleToUser(ctx context.Context, userID, orgID int, roleName string) error

	// RemoveRoleFromUser removes a named role from a user within an organisation.
	RemoveRoleFromUser(ctx context.Context, userID, orgID int, roleName string) error

	// AssignUserToOrg adds a user to a named organisation and grants them the given role names.
	AssignUserToOrg(ctx context.Context, userID int, orgName string, roleNames []string) error

	// SetUserOrgRoles replaces all roles for a user in a named organisation.
	// Pass an empty slice to remove all roles while keeping org membership.
	SetUserOrgRoles(ctx context.Context, userID int, orgName string, roleNames []string) error

	// ListOrganisations returns all organisations ordered by name.
	ListOrganisations(ctx context.Context) ([]Organisation, error)

	// GetOrganisation returns a single organisation by name. Returns nil if not found.
	GetOrganisation(ctx context.Context, name string) (*Organisation, error)

	// CreateOrganisation inserts a new organisation. Sets org.ID on success.
	CreateOrganisation(ctx context.Context, org *Organisation) error

	// UpdateOrganisation updates an existing organisation identified by name.
	// The name is immutable and is only used as the lookup key; only
	// DisplayName, Active, and Area are written.
	UpdateOrganisation(ctx context.Context, name string, org *Organisation) error

	// DeleteOrganisation removes an organisation. Refuses to delete "default".
	DeleteOrganisation(ctx context.Context, name string) error

	// ListAPIKeys returns API key records for an organisation. Callers that
	// expose these records must not return raw key values to clients.
	ListAPIKeys(ctx context.Context, orgName string) ([]APIKey, error)

	// CreateAPIKey generates and stores a new named API key for an organisation.
	// The returned APIKey includes the raw key value.
	CreateAPIKey(ctx context.Context, orgName, name string) (*APIKey, error)

	// DeleteAPIKey removes an API key by ID within an organisation.
	DeleteAPIKey(ctx context.Context, orgName string, id int) error

	// GetAPIKeyOrg looks up which organisation owns the given raw key value.
	// Returns ("", nil) when the key is not found.
	GetAPIKeyOrg(ctx context.Context, key string) (string, error)

	// ListNotificationProfiles returns all notification profiles for an organisation.
	ListNotificationProfiles(ctx context.Context, org string) ([]NotificationProfile, error)

	// GetNotificationProfile returns a single notification profile by ID.
	GetNotificationProfile(ctx context.Context, org string, id int) (*NotificationProfile, error)

	// GetNotificationProfileByName returns a profile by name within an organisation.
	GetNotificationProfileByName(ctx context.Context, org string, name string) (*NotificationProfile, error)

	// ResolveNotificationID returns the integer ID for a notification profile within
	// an org, given its canonical name (e.g. "SysAdmin", "Manager", "Technician").
	// Returns 0 if the profile is not found.
	ResolveNotificationID(ctx context.Context, org, name string) (int, error)

	// CreateNotificationProfile inserts a new notification profile.
	CreateNotificationProfile(ctx context.Context, org string, p *NotificationProfile) error

	// UpdateNotificationProfile updates an existing notification profile.
	UpdateNotificationProfile(ctx context.Context, org string, id int, p *NotificationProfile) error

	// DeleteNotificationProfile removes a notification profile by ID.
	DeleteNotificationProfile(ctx context.Context, org string, id int) error

	// GetNotificationRecipients returns the deduplicated list of users matching
	// a profile's roles and explicit user list.
	GetNotificationRecipients(ctx context.Context, org string, profileID int) ([]NotificationRecipient, error)

	// ListPDFTemplates returns all PDF report templates for an organisation.
	ListPDFTemplates(ctx context.Context, org string) ([]PDFTemplate, error)

	// GetPDFTemplate returns a single PDF template by ID. Returns nil if not found.
	GetPDFTemplate(ctx context.Context, org string, id string) (*PDFTemplate, error)

	// CreatePDFTemplate inserts a new PDF template. Sets t.ID on success.
	CreatePDFTemplate(ctx context.Context, org string, t *PDFTemplate) error

	// UpdatePDFTemplate replaces an existing PDF template identified by ID.
	UpdatePDFTemplate(ctx context.Context, org string, id string, t *PDFTemplate) error

	// DeletePDFTemplate removes a PDF template by ID.
	DeletePDFTemplate(ctx context.Context, org string, id string) error

	// InsertMetrics writes one or more timestamped metric values for an organisation.
	InsertMetrics(ctx context.Context, orgName string, entries []MetricEntry) error

	// QueryMetricsRange returns time-ordered series for a device over [start, end].
	// If end is zero, defaults to time.Now(). metrics lists the metric names to return.
	QueryMetricsRange(ctx context.Context, orgName, deviceName string,
		metrics []string, start, end time.Time) ([]MetricSeries, error)

	// QueryMetricsByTagPaths returns time-ordered series for metrics whose stored
	// "device.metric" path exactly matches one of the given tag paths.
	QueryMetricsByTagPaths(ctx context.Context, orgName string,
		tagPaths []string, start, end time.Time) ([]MetricSeries, error)

	// QueryMetricsSince returns series for the listed metrics with time > startTime.
	QueryMetricsSince(ctx context.Context, orgName, deviceName string,
		metrics []string, startMetric string, startTime time.Time) ([]MetricSeries, error)

	// ConfigureMetricsRetention updates the TimescaleDB data retention policy.
	ConfigureMetricsRetention(ctx context.Context, retention time.Duration) error

	// ListTagCalcs returns all tag calcs for an organisation.
	ListTagCalcs(ctx context.Context, org string) ([]TagCalc, error)

	// GetTagCalc returns a single tag calc by ID. Returns nil if not found.
	GetTagCalc(ctx context.Context, org string, id int) (*TagCalc, error)

	// CreateTagCalc inserts a new tag calc. Sets s.ID, s.CreatedAt, s.UpdatedAt on success.
	CreateTagCalc(ctx context.Context, org string, s *TagCalc) error

	// UpdateTagCalc replaces an existing tag calc identified by ID.
	UpdateTagCalc(ctx context.Context, org string, id int, s *TagCalc) error

	// DeleteTagCalc removes a tag calc by ID.
	DeleteTagCalc(ctx context.Context, org string, id int) error

	// BackupAdapter returns a backups.Adapter backed by this database connection.
	// Used by the scheduler to run backup tasks without hardcoding the DB type.
	BackupAdapter() backups.Adapter

	// ListScheduledTasks returns all scheduled tasks for an organisation.
	ListScheduledTasks(ctx context.Context, org string) ([]ScheduledTask, error)

	// GetScheduledTask returns a single scheduled task by ID. Returns nil if not found.
	GetScheduledTask(ctx context.Context, org string, id string) (*ScheduledTask, error)

	// CreateScheduledTask inserts a new scheduled task. Sets t.ID on success.
	CreateScheduledTask(ctx context.Context, org string, t *ScheduledTask) error

	// UpdateScheduledTask replaces an existing scheduled task identified by ID.
	UpdateScheduledTask(ctx context.Context, org string, id string, t *ScheduledTask) error

	// DeleteScheduledTask removes a scheduled task by ID.
	DeleteScheduledTask(ctx context.Context, org string, id string) error

	// UpdateScheduledTaskStatus updates last_run_at, last_run_status, last_run_message.
	UpdateScheduledTaskStatus(ctx context.Context, id string, status string, message string, runAt time.Time) error

	// AppendScheduleRunLog inserts a run log entry. Sets entry.ID on success.
	AppendScheduleRunLog(ctx context.Context, entry *ScheduleRunLog) error

	// UpdateScheduleRunLog updates completed_at, status, message, output_path for a run log entry.
	UpdateScheduleRunLog(ctx context.Context, id int, completedAt time.Time, status, message, outputPath string) error

	// ListScheduleRunLog returns the most recent run log entries for a scheduled task.
	ListScheduleRunLog(ctx context.Context, scheduleID string, limit int) ([]ScheduleRunLog, error)
}

// OrgArea represents the geographic bounding box of an organisation's site.
// Coordinates are decimal degrees (WGS-84).
type OrgArea struct {
	North float64 `json:"north"`
	South float64 `json:"south"`
	East  float64 `json:"east"`
	West  float64 `json:"west"`
}

// Organisation represents a tenant organisation.
// Name is the immutable slug used as the RTDB root node key.
// DisplayName is the human-friendly label shown in reports and emails.
type Organisation struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	DisplayName string   `json:"displayName"`
	Active      bool     `json:"active"`
	Logo        string   `json:"logo,omitempty"`
	Favicon     string   `json:"favicon,omitempty"`
	Area        *OrgArea `json:"area,omitempty"`
}

// RolePermissions represents a role's permission mappings.
type RolePermissions struct {
	Role   string          `json:"role"`
	UI     json.RawMessage `json:"ui"`
	Server json.RawMessage `json:"server"`
}

// Role represents a user role definition.
type Role struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// User represents a user account (never includes the password hash).
type User struct {
	ID                  int             `json:"id"`
	FirstName           string          `json:"firstName"`
	LastName            string          `json:"lastName"`
	LoginName           string          `json:"loginName"`
	Email               string          `json:"email"`
	NotificationOptions json.RawMessage `json:"notificationOptions,omitempty"`
	Active              bool            `json:"active"`
	LastLogin           *time.Time      `json:"lastLogin,omitempty"`
	TokenVersion        int             `json:"-"`
	CreatedAt           time.Time       `json:"createdAt"`
	Orgs                []UserOrg       `json:"orgs,omitempty"`
}

// UserOrg represents a user's membership in an organisation with their assigned roles.
type UserOrg struct {
	OrgID   int      `json:"orgId"`
	OrgName string   `json:"orgName"`
	Roles   []string `json:"roles"`
}

// DashboardMeta contains dashboard metadata without the widgets payload.
// Used for listing dashboards in the sidebar and config editor.
type DashboardMeta struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Variation   string `json:"variation"`
	DeviceType  string `json:"deviceType"`
	Permission  string `json:"permission"`
	IsCategory  bool   `json:"isCategory"`
	ParentID    *int   `json:"parentId,omitempty"`
	SortOrder   int    `json:"sortOrder"`
}

const (
	StarterDashboardName        = "DASHBOARD"
	StarterMonitoringCategory   = "MONTORING"
	StarterTagViewName          = "Tag View"
	StarterDashboardWidgetsJSON = `[{"id":"help-manual","type":"manual-widget","x":0,"y":0,"w":24,"h":28,"config":{}}]`
	StarterTagViewWidgetsJSON   = `[{"id":"tag-browser","type":"tags-manager-widget","x":0,"y":0,"w":24,"h":28,"config":{}}]`
)

// EventFilter specifies criteria for querying event entries.
type EventFilter struct {
	OrgName        string
	UserID         int
	Severity       string
	Device         string
	NotificationID int
	Search         string
	StartTime      *time.Time
	EndTime        *time.Time
	AfterID        int64
	Limit          int
}

// MetricEntry is a single timestamped metric value to be inserted.
type MetricEntry struct {
	DeviceName string
	MetricName string
	Timestamp  time.Time
	Value      float32
}

// MetricPoint is a single time/value pair in a series.
type MetricPoint struct {
	Timestamp time.Time
	Value     float32
}

// MetricSeries groups data points for a single named metric.
type MetricSeries struct {
	Name string
	Data []MetricPoint
}

// NotificationProfile defines who should receive a notification.
type NotificationProfile struct {
	ID          int       `json:"id"`
	OrgName     string    `json:"orgName"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Roles       []string  `json:"roles"`
	Users       []int     `json:"users"`
	AckRequired bool      `json:"ackRequired"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// NotificationRecipient represents a user who should receive a notification.
type NotificationRecipient struct {
	ID                  int             `json:"id"`
	FirstName           string          `json:"firstName"`
	LastName            string          `json:"lastName"`
	Email               string          `json:"email"`
	NotificationOptions json.RawMessage `json:"notificationOptions,omitempty"`
}

// TagCalc represents a user-defined computed tag expression.
type TagCalc struct {
	ID              int       `json:"id"`
	OrgName         string    `json:"orgName"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	OutputTag       string    `json:"outputTag"` // org-relative dot-notation: "CUSTOM.System.Health"
	Expression      string    `json:"expression"`
	IntervalSeconds int       `json:"intervalSeconds"` // evaluation frequency
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// PDFTemplate represents a saved report template.
type PDFTemplate struct {
	ID           string          `json:"id"`
	OrgName      string          `json:"orgName"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	TemplateJSON json.RawMessage `json:"templateJson"`
	Variables    json.RawMessage `json:"variables"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

// APIKey represents a named API key belonging to an organisation.
// The Key field may hold the raw value when a key is first created. Treat it
// as secret material and do not return it from list/read endpoints.
type APIKey struct {
	ID        int       `json:"id"`
	OrgName   string    `json:"orgName"`
	Name      string    `json:"name"`
	Key       string    `json:"key"`
	KeyPrefix string    `json:"keyPrefix,omitempty"`
	KeyLast4  string    `json:"keyLast4,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// AgentToken represents a named bearer token for agent/MCP/API access.
// The Token field may hold the raw value when a token is first created. Treat it
// as secret material and do not return it from list/read endpoints.
type AgentToken struct {
	ID              int        `json:"id"`
	OrgName         string     `json:"orgName"`
	UserID          int        `json:"userId"`
	UserLoginName   string     `json:"userLoginName,omitempty"`
	UserDisplayName string     `json:"userDisplayName,omitempty"`
	Name            string     `json:"name"`
	Token           string     `json:"token,omitempty"`
	TokenPrefix     string     `json:"tokenPrefix,omitempty"`
	TokenLast4      string     `json:"tokenLast4,omitempty"`
	Roles           []string   `json:"roles"`
	CreatedAt       time.Time  `json:"createdAt"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt      *time.Time `json:"lastUsedAt,omitempty"`
}

// ScheduledTask represents a recurring job configured by a user.
type ScheduledTask struct {
	ID             string          `json:"id"`
	OrgName        string          `json:"orgName"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	TaskType       string          `json:"taskType"` // "report"|"backup"|"shell"|"yaegi"|"command"
	TaskConfig     json.RawMessage `json:"taskConfig"`
	Schedule       string          `json:"schedule"` // 5-field cron expression
	Enabled        bool            `json:"enabled"`
	LastRunAt      *time.Time      `json:"lastRunAt,omitempty"`
	LastRunStatus  string          `json:"lastRunStatus"` // ""|"running"|"ok"|"error"
	LastRunMessage string          `json:"lastRunMessage"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

// ScheduleRunLog records a single execution of a scheduled task.
type ScheduleRunLog struct {
	ID          int        `json:"id"`
	ScheduleID  string     `json:"scheduleId"`
	OrgName     string     `json:"orgName"`
	FiredAt     time.Time  `json:"firedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	Status      string     `json:"status"` // "ok"|"error"
	Message     string     `json:"message"`
	OutputPath  string     `json:"outputPath"`
}

// Dashboard contains the full dashboard data including widgets.
type Dashboard struct {
	ID          int             `json:"id,omitempty"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Icon        string          `json:"icon"`
	Variation   string          `json:"variation"`
	DeviceType  string          `json:"deviceType"`
	Permission  string          `json:"permission"`
	IsCategory  bool            `json:"isCategory"`
	ParentID    *int            `json:"parentId,omitempty"`
	SortOrder   int             `json:"sortOrder"`
	Widgets     json.RawMessage `json:"widgets"`
}
