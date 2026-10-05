package domain

import "time"

const ProtocolVersion = "1.0"

type Account struct {
	Removed     bool      `json:"removed,omitempty"`
	ID          string    `json:"id"`
	Namespace   string    `json:"namespace"`
	DisplayName string    `json:"displayName"`
	Username    string    `json:"username,omitempty"`
	Phone       string    `json:"phone,omitempty"`
	UserID      string    `json:"userId,omitempty"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Chat struct {
	Archived      bool      `json:"archived"`
	LastMessageAt time.Time `json:"lastMessageAt,omitempty"`
	PinnedOrder   int       `json:"pinnedOrder,omitempty"`
	FolderIDs     []string  `json:"folderIds,omitempty"`
	AccountID     string    `json:"accountId"`
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	VisibleName   string    `json:"visibleName"`
	Username      string    `json:"username,omitempty"`
	Topics        []Topic   `json:"topics,omitempty"`
}

type ChatFolder struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Emoticon  string   `json:"emoticon,omitempty"`
	ChatIDs   []string `json:"chatIds"`
	PinnedIDs []string `json:"pinnedIds"`
}

type Topic struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type Media struct {
	TopicID    string    `json:"topicId,omitempty"`
	AccountID  string    `json:"accountId"`
	ChatID     string    `json:"chatId"`
	MessageID  string    `json:"messageId"`
	MediaID    string    `json:"mediaId"`
	GroupedID  string    `json:"groupedId,omitempty"`
	Kind       string    `json:"kind"`
	FileName   string    `json:"fileName"`
	Extension  string    `json:"extension,omitempty"`
	MIME       string    `json:"mime,omitempty"`
	Size       int64     `json:"size"`
	Caption    string    `json:"caption,omitempty"`
	Date       time.Time `json:"date"`
	Duration   int       `json:"duration,omitempty"`
	Width      int       `json:"width,omitempty"`
	Height     int       `json:"height,omitempty"`
	ThumbPath  string    `json:"thumbPath,omitempty"`
	LocalPath  string    `json:"localPath,omitempty"`
	Downloaded bool      `json:"downloaded"`
}

type Rule struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	AccountID      string    `json:"accountId"`
	ChatID         string    `json:"chatId"`
	TopicID        string    `json:"topicId,omitempty"`
	From           time.Time `json:"from,omitempty"`
	To             time.Time `json:"to,omitempty"`
	RecentDays     int       `json:"recentDays,omitempty"`
	LastN          int       `json:"lastN,omitempty"`
	MinMessageID   int64     `json:"minMessageId,omitempty"`
	MaxMessageID   int64     `json:"maxMessageId,omitempty"`
	Kinds          []string  `json:"kinds,omitempty"`
	IncludeExt     []string  `json:"includeExt,omitempty"`
	ExcludeExt     []string  `json:"excludeExt,omitempty"`
	IncludeKeyword string    `json:"includeKeyword,omitempty"`
	ExcludeKeyword string    `json:"excludeKeyword,omitempty"`
	MinFileSize    int64     `json:"minFileSize,omitempty"`
	MaxFileSize    int64     `json:"maxFileSize,omitempty"`
	MaxFiles       int       `json:"maxFiles,omitempty"`
	MaxTotalSize   int64     `json:"maxTotalSize,omitempty"`
	Order          string    `json:"order"`
	Timezone       string    `json:"timezone"`
	RootDir        string    `json:"rootDir"`
	Template       string    `json:"template"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type PlanItem struct {
	Media       Media  `json:"media"`
	TargetPath  string `json:"targetPath"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
	Existing    bool   `json:"existing"`
	Selected    bool   `json:"selected"`
	CanonicalID string `json:"canonicalId"`
}

type DownloadPlan struct {
	From          time.Time  `json:"from,omitempty"`
	To            time.Time  `json:"to,omitempty"`
	ID            string     `json:"id"`
	RuleID        string     `json:"ruleId,omitempty"`
	AccountID     string     `json:"accountId"`
	ChatID        string     `json:"chatId"`
	CreatedAt     time.Time  `json:"createdAt"`
	MessageCount  int        `json:"messageCount"`
	UniqueFiles   int        `json:"uniqueFiles"`
	ExistingFiles int        `json:"existingFiles"`
	SelectedFiles int        `json:"selectedFiles"`
	SelectedBytes int64      `json:"selectedBytes"`
	Items         []PlanItem `json:"items"`
}

type Job struct {
	SpeedBytesPerSecond float64   `json:"speedBytesPerSecond"`
	ID                  string    `json:"id"`
	PlanID              string    `json:"planId"`
	AccountID           string    `json:"accountId"`
	ChatID              string    `json:"chatId"`
	State               string    `json:"state"`
	TotalFiles          int       `json:"totalFiles"`
	DoneFiles           int       `json:"doneFiles"`
	FailedFiles         int       `json:"failedFiles"`
	TotalBytes          int64     `json:"totalBytes"`
	DoneBytes           int64     `json:"doneBytes"`
	Error               string    `json:"error,omitempty"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type JobItem struct {
	DownloadedBytes     int64   `json:"downloadedBytes"`
	SpeedBytesPerSecond float64 `json:"speedBytesPerSecond"`
	JobID               string  `json:"jobId"`
	MediaID             string  `json:"mediaId"`
	ChatID              string  `json:"chatId"`
	MessageID           string  `json:"messageId"`
	TargetPath          string  `json:"targetPath"`
	StagingPath         string  `json:"stagingPath,omitempty"`
	State               string  `json:"state"`
	Attempts            int     `json:"attempts"`
	Size                int64   `json:"size"`
	Error               string  `json:"error,omitempty"`
}

type EngineVersion struct {
	Version     string    `json:"version"`
	Path        string    `json:"path"`
	SHA256      string    `json:"sha256,omitempty"`
	InstalledAt time.Time `json:"installedAt"`
	Active      bool      `json:"active"`
}

type Settings struct {
	ChatOrder        string `json:"chatOrder"`
	MediaOrder       string `json:"mediaOrder"`
	DataDir          string `json:"dataDir"`
	DownloadRoot     string `json:"downloadRoot"`
	Proxy            string `json:"proxy,omitempty"`
	NTP              string `json:"ntp,omitempty"`
	ReconnectTimeout string `json:"reconnectTimeout"`
	TaskDelay        string `json:"taskDelay"`
	FileConcurrency  int    `json:"fileConcurrency"`
	Retries          int    `json:"retries"`
	MinFreeBytes     int64  `json:"minFreeBytes"`
	CacheMaxBytes    int64  `json:"cacheMaxBytes"`
	LogLevel         string `json:"logLevel"`
	EnginePath       string `json:"enginePath,omitempty"`
	EngineVersion    string `json:"engineVersion,omitempty"`
}
