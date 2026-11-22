package repo

import (
	"errors"

	"gorm.io/gorm"

	"go-backend/internal/store/model"
)

func (r *Repository) CreateAPIToken(t *model.APIToken) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Create(t).Error
}

func (r *Repository) ListAPITokens() ([]model.APIToken, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var rows []model.APIToken
	err := r.db.Order("id DESC").Find(&rows).Error
	return rows, err
}

func (r *Repository) GetAPITokenByHash(hash string) (*model.APIToken, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var row model.APIToken
	err := r.db.Where("token_hash = ?", hash).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}

func (r *Repository) RevokeAPIToken(id int64) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("repository not initialized")
	}
	res := r.db.Model(&model.APIToken{}).Where("id = ?", id).Update("revoked", 1)
	return res.RowsAffected, res.Error
}

func (r *Repository) DeleteAPIToken(id int64) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("repository not initialized")
	}
	res := r.db.Where("id = ?", id).Delete(&model.APIToken{})
	return res.RowsAffected, res.Error
}

func (r *Repository) TouchAPITokenUsage(id int64, now int64, ip string) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.APIToken{}).Where("id = ?", id).Updates(map[string]interface{}{
		"last_used_at": now,
		"last_used_ip": ip,
	}).Error
}
