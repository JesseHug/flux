package model

type APIToken struct {
	ID          int64  `gorm:"primaryKey;autoIncrement"`
	Name        string `gorm:"type:varchar(100);not null"`
	Prefix      string `gorm:"type:varchar(24);not null"`
	TokenHash   string `gorm:"column:token_hash;type:varchar(64);not null;uniqueIndex"`
	Scope       string `gorm:"type:varchar(20);not null;default:'operator'"`
	AllowedIPs  string `gorm:"column:allowed_ips;type:text;not null;default:''"`
	UserID      int64  `gorm:"column:user_id;not null;index"`
	ExpiresAt   int64  `gorm:"column:expires_at;not null;default:0"`
	LastUsedAt  int64  `gorm:"column:last_used_at;not null;default:0"`
	LastUsedIP  string `gorm:"column:last_used_ip;type:varchar(64);not null;default:''"`
	Revoked     int    `gorm:"not null;default:0"`
	CreatedTime int64  `gorm:"column:created_time;not null"`
}

func (APIToken) TableName() string { return "api_token" }
