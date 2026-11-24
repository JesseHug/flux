package repo

import (
	"errors"

	"go-backend/internal/store/model"
)

func (r *Repository) ListCFPools() ([]model.CFPool, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var out []model.CFPool
	err := r.db.Order("id ASC").Find(&out).Error
	return out, err
}

func (r *Repository) GetCFPool(id int64) (*model.CFPool, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var out model.CFPool
	res := r.db.Where("id = ?", id).Limit(1).Find(&out)
	if res.Error != nil || res.RowsAffected == 0 {
		return nil, res.Error
	}
	return &out, nil
}

// SaveCFPool stores the user-editable fields (status fields are updated by
// UpdateCFPoolStatus).
func (r *Repository) SaveCFPool(p *model.CFPool) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	if p.ID == 0 {
		return r.db.Create(p).Error
	}
	return r.db.Model(&model.CFPool{}).Where("id = ?", p.ID).Updates(map[string]interface{}{
		"name":           p.Name,
		"kind":           p.Kind,
		"url":            p.URL,
		"raw_text":       p.RawText,
		"user_agent":     p.UserAgent,
		"interval_hours": p.IntervalHours,
		"default_port":   p.DefaultPort,
		"max_entries":    p.MaxEntries,
		"ip_filter":      p.IPFilter,
		"enabled":        p.Enabled,
		"updated_time":   p.UpdatedTime,
	}).Error
}

func (r *Repository) UpdateCFPoolStatus(id int64, fields map[string]interface{}) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.CFPool{}).Where("id = ?", id).Updates(fields).Error
}

func (r *Repository) DeleteCFPool(id int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Where("id = ?", id).Delete(&model.CFPool{}).Error
}
