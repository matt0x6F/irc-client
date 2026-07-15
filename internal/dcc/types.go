package dcc

import "time"

// Direction identifies which side owns the file being transferred.
type Direction string

const (
	DirectionIncoming Direction = "incoming"
	DirectionOutgoing Direction = "outgoing"
)

// Status is the user-visible lifecycle state for a file transfer.
type Status string

const (
	StatusOffered      Status = "offered"
	StatusQueued       Status = "queued"
	StatusNegotiating  Status = "negotiating"
	StatusConnecting   Status = "connecting"
	StatusTransferring Status = "transferring"
	StatusCompleted    Status = "completed"
	StatusFailed       Status = "failed"
	StatusCanceled     Status = "canceled"
	StatusDeclined     Status = "declined"
	StatusResumable    Status = "resumable"
)

func (s Status) Active() bool {
	switch s {
	case StatusOffered, StatusQueued, StatusNegotiating, StatusConnecting, StatusTransferring:
		return true
	default:
		return false
	}
}

// ConnectionMode controls how an outgoing direct connection is negotiated.
type ConnectionMode string

const (
	ConnectionAutomatic ConnectionMode = "automatic"
	ConnectionClassic   ConnectionMode = "classic"
	ConnectionPassive   ConnectionMode = "passive"
)

// HistoryRetention is deliberately string-valued at the Wails boundary.
type HistoryRetention string

const (
	HistoryForever HistoryRetention = "forever"
	History30Days  HistoryRetention = "30_days"
	HistoryNone    HistoryRetention = "none"
)

// Settings are the validated direct-file-transfer preferences.
type Settings struct {
	Enabled           bool             `json:"enabled"`
	DownloadDirectory string           `json:"downloadDirectory"`
	HistoryRetention  HistoryRetention `json:"historyRetention"`
	ConnectionMode    ConnectionMode   `json:"connectionMode"`
	AdvertisedAddress string           `json:"advertisedAddress"`
	PortMin           int              `json:"portMin"`
	PortMax           int              `json:"portMax"`
}

// Transfer is the manager's complete record. LocalPath and PartialPath must not
// cross the Wails boundary; use View for frontend data.
type Transfer struct {
	ID               string
	NetworkID        int64
	NetworkName      string
	Peer             string
	Direction        Direction
	Filename         string
	LocalPath        string
	PartialPath      string
	TotalBytes       int64
	TransferredBytes int64
	Status           Status
	Error            string
	Address          string
	Port             int
	Token            string
	ResumeOffset     int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CompletedAt      *time.Time
	SpeedBPS         int64
	ETASeconds       int64
	FileAvailable    bool
	Resumable        bool
}

// View is the safe, typed model sent to React.
type View struct {
	ID               string     `json:"id"`
	NetworkID        int64      `json:"networkId"`
	NetworkName      string     `json:"networkName"`
	Peer             string     `json:"peer"`
	Direction        Direction  `json:"direction"`
	Filename         string     `json:"filename"`
	TotalBytes       int64      `json:"totalBytes"`
	TransferredBytes int64      `json:"transferredBytes"`
	Status           Status     `json:"status"`
	Error            string     `json:"error,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	CompletedAt      *time.Time `json:"completedAt,omitempty"`
	SpeedBPS         int64      `json:"speedBps"`
	ETASeconds       int64      `json:"etaSeconds"`
	FileAvailable    bool       `json:"fileAvailable"`
	Resumable        bool       `json:"resumable"`
}

func (t Transfer) View() View {
	return View{
		ID: t.ID, NetworkID: t.NetworkID, NetworkName: t.NetworkName,
		Peer: t.Peer, Direction: t.Direction, Filename: t.Filename,
		TotalBytes: t.TotalBytes, TransferredBytes: t.TransferredBytes,
		Status: t.Status, Error: t.Error, CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt, CompletedAt: t.CompletedAt,
		SpeedBPS: t.SpeedBPS, ETASeconds: t.ETASeconds,
		FileAvailable: t.FileAvailable, Resumable: t.Resumable,
	}
}

type Event struct {
	Type     string `json:"type"` // upsert|remove|reset
	Transfer *View  `json:"transfer,omitempty"`
	ID       string `json:"id,omitempty"`
}
