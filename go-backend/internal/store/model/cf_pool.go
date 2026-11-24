package model

const (
	CFPoolKindManual = "manual"
	CFPoolKindURL    = "url"

	CFPoolFilterAll    = "all"
	CFPoolFilterIPv4   = "ipv4"
	CFPoolFilterIPv6   = "ipv6"
	CFPoolFilterDomain = "domain"
)

// CFPool stores a reusable pool of Cloudflare optimal IPs or CNAME domains.
// Pools may be maintained manually (pasted lists) or synced periodically from
// a remote URL.
type CFPool struct {
	ID            int64  `gorm:"primaryKey;autoIncrement"`
	Name          string `gorm:"type:varchar(100);not null"`
	Kind          string `gorm:"type:varchar(16);not null;default:'manual'"`
	URL           string `gorm:"type:text;not null;default:''"`
	RawText       string `gorm:"column:raw_text;type:text;not null;default:''"`
	UserAgent     string `gorm:"column:user_agent;type:varchar(255);not null;default:''"`
	IntervalHours int    `gorm:"column:interval_hours;not null;default:0"`
	DefaultPort   int    `gorm:"column:default_port;not null;default:443"`
	MaxEntries    int    `gorm:"column:max_entries;not null;default:10"`
	IPFilter      string `gorm:"column:ip_filter;type:varchar(16);not null;default:'all'"`
	CachedEntries string `gorm:"column:cached_entries;type:text;not null;default:'[]'"`
	EntryCount    int    `gorm:"column:entry_count;not null;default:0"`
	Enabled       int    `gorm:"not null;default:1"`
	LastFetch     int64  `gorm:"column:last_fetch;not null;default:0"`
	LastError     string `gorm:"column:last_error;type:text;not null;default:''"`
	CreatedTime   int64  `gorm:"column:created_time;not null"`
	UpdatedTime   int64  `gorm:"column:updated_time;not null"`
}

func (CFPool) TableName() string { return "cf_pool" }
