package data

type BackupInfo struct {
	Archives []Archive `json:"archive"`
	Backups  []Backup  `json:"backup"`
}

// Archive contains information about the current WALs required to rebuild the
// database since the oldest backup and the repo used.
type Archive struct {
	DBInfo DatabaseInfo `json:"database"`
	ID     string       `json:"id"`
	MaxWal string       `json:"max"`
	MinWal string       `json:"min"`
}
type DatabaseInfo struct {
	ID      int `json:"id"`
	RepoKey int `json:"repo-key"`
}

type StopStartArchives struct {
	Start string `json:"start"`
	Stop  string `json:"stop"`
}
type BackrestInfo struct {
	Format  int    `json:"format"`
	Version string `json:"version"`
}
type SizeInformation struct {
	Delta      int64              `json:"delta"`
	Repository RepositorySizeInfo `json:"repository"`
	Size       int64              `json:"size"`
}
type RepositorySizeInfo struct {
	Delta int64 `json:"delta"`
	Size  int64 `json:"size"`
}
type BackupTimestamps struct {
	Start int64 `json:"start"`
	Stop  int64 `json:"stop"`
}
type BackupType string

const (
	Full         BackupType = "full"
	Incremental  BackupType = "incr"
	Differential BackupType = "diff"
)

type Backup struct {
	BackupMandatoryArchives StopStartArchives `json:"archive"`
	PgBackrestUsed          BackrestInfo      `json:"backrest"`
	DatabaseInfo            DatabaseInfo      `json:"database"`
	ErrorOccurred           bool              `json:"error"`
	SizeDetails             SizeInformation   `json:"info"`
	Label                   string            `json:"label"`
	Prior                   *string           `json:"prior"`
	Reference               []string          `json:"reference"`
	StopStartTime           BackupTimestamps  `json:"timestamp"`
	Type                    BackupType        `json:"type"`
}
