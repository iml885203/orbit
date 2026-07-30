package daemon

// The server uses the public daemon package as its wire contract and
// host-capability boundary.

import (
	pub "github.com/iml885203/orbit/daemon"
)

type (
	APIResponse            = pub.APIResponse
	UpRequest              = pub.UpRequest
	DownRequest            = pub.DownRequest
	StatusResponse         = pub.StatusResponse
	ResourceKind           = pub.ResourceKind
	SidecarInfo            = pub.SidecarInfo
	ResourceStatus         = pub.ResourceStatus
	ResourceRestart        = pub.ResourceRestart
	ResourcePortConflict   = pub.ResourcePortConflict
	HealthProgressInfo     = pub.HealthProgressInfo
	LogsResponse           = pub.LogsResponse
	DoctorCheckStatus      = pub.DoctorCheckStatus
	DoctorCheck            = pub.DoctorCheck
	DoctorResponse         = pub.DoctorResponse
	TraceLogLine           = pub.TraceLogLine
	TraceLogsResponse      = pub.TraceLogsResponse
	EnvInfo                = pub.EnvInfo
	EnvsResponse           = pub.EnvsResponse
	VersionResponse        = pub.VersionResponse
	EdgeDetachRequest      = pub.EdgeDetachRequest
	ServiceModeRequest     = pub.ServiceModeRequest
	EnvToggleUpdateRequest = pub.EnvToggleUpdateRequest
	SettingsResponse       = pub.SettingsResponse
	Settings               = pub.Settings
	SettingsChange         = pub.SettingsChange
	SettingsPUTHook        = pub.SettingsPUTHook
	ResourceSnapshot       = pub.ResourceSnapshot
	ResourcesResponse      = pub.ResourcesResponse
	ResourceContributor    = pub.ResourceContributor
	ContainerOps           = pub.ContainerOps
	ServiceRestarter       = pub.ServiceRestarter
	Client                 = pub.Client
	HostToolCheck          = pub.HostToolCheck
	HostVersionRequirement = pub.HostVersionRequirement
)

const (
	cliOriginHeader       = pub.CLIOriginHeader
	ResourceKindContainer = pub.ResourceKindContainer
	ResourceKindService   = pub.ResourceKindService
	CheckPass             = pub.CheckPass
	CheckFail             = pub.CheckFail
	CheckWarn             = pub.CheckWarn
	CheckInfo             = pub.CheckInfo
	ResourceSchemaVersion = pub.ResourceSchemaVersion
)

var (
	writeJSON                   = pub.WriteJSON
	requireMethod               = pub.RequireMethod
	OrbitDir                    = pub.OrbitDir
	LoadSettings                = pub.LoadSettings
	DefaultSettingsPath         = pub.DefaultSettingsPath
	SocketHTTPClient            = pub.SocketHTTPClient
	SocketHTTPClientWithTimeout = pub.SocketHTTPClientWithTimeout
	NewClient                   = pub.NewClient
	DefaultSocketPath           = pub.DefaultSocketPath
	ValidateSocketPath          = pub.ValidateSocketPath
	WorkspaceRootCheck          = pub.WorkspaceRootCheck
	WorkspaceRootFromEnv        = pub.WorkspaceRootFromEnv
	Cleanup                     = pub.Cleanup
	DashboardPort               = pub.DashboardPort
	WritePID                    = pub.WritePID
	RemovePID                   = pub.RemovePID
	ReadPID                     = pub.ReadPID
	IsProcessAlive              = pub.IsProcessAlive
	RedirectLogToFile           = pub.RedirectLogToFile
	SocketPath                  = pub.SocketPath
	ListenDashboard             = pub.ListenDashboard
	DefaultPIDPath              = pub.DefaultPIDPath
	DefaultLogPath              = pub.DefaultLogPath
)
