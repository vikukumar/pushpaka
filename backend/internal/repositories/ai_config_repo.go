package repositories

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/vikukumar/pushpaka/pkg/basemodel"
	"github.com/vikukumar/pushpaka/pkg/models"
)

// AIConfigRepository manages AI configuration, RAG documents, and monitoring alerts.
type AIConfigRepository struct {
	db *gorm.DB
}

func NewAIConfigRepository(db *gorm.DB) *AIConfigRepository {
	return &AIConfigRepository{db: db}
}

// ─── AI Config ────────────────────────────────────────────────────────────────

func (r *AIConfigRepository) GetByUserID(userID string) (*models.AIConfig, error) {
	cfg, err := basemodel.First[models.AIConfig](r.db, "user_id = ?", userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return cfg, nil
}

func (r *AIConfigRepository) Upsert(cfg *models.AIConfig) error {
	basemodel.EnsureSynced[models.AIConfig](r.db)
	if cfg.ID == "" {
		cfg.ID = uuid.New().String()
		cfg.CreatedAt = time.Now().UTC()
	}
	cfg.UpdatedAt = time.Now().UTC()

	// Keep existing api_key if new is empty (like previous SQL implementation)
	return r.db.Exec(`
		INSERT INTO ai_configs
			(id, user_id, provider, api_key, model, base_url, system_prompt,
			 monitoring_enabled, monitoring_interval, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET
			provider=excluded.provider,
			api_key=CASE WHEN excluded.api_key='' THEN ai_configs.api_key ELSE excluded.api_key END,
			model=excluded.model,
			base_url=excluded.base_url,
			system_prompt=excluded.system_prompt,
			monitoring_enabled=excluded.monitoring_enabled,
			monitoring_interval=excluded.monitoring_interval,
			updated_at=excluded.updated_at
	`, cfg.ID, cfg.UserID, cfg.Provider, cfg.APIKey, cfg.Model, cfg.BaseURL,
		cfg.SystemPrompt, cfg.MonitoringEnabled, cfg.MonitoringInterval, cfg.CreatedAt.Format(time.RFC3339), cfg.UpdatedAt.Format(time.RFC3339)).Error
}

// ─── RAG Documents ────────────────────────────────────────────────────────────

func (r *AIConfigRepository) ListRAG(userID string) ([]models.RAGDocument, error) {
	return basemodel.Query[models.RAGDocument](r.db, "user_id = ?", userID)
}

func (r *AIConfigRepository) CreateRAG(doc *models.RAGDocument) error {
	return basemodel.Add(r.db, doc)
}

func (r *AIConfigRepository) DeleteRAG(id, userID string) error {
	basemodel.EnsureSynced[models.RAGDocument](r.db)
	result := r.db.Where("id = ? AND user_id = ?", id, userID).Delete(&models.RAGDocument{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("document not found")
	}
	return nil
}

// ─── AI Monitor Alerts ────────────────────────────────────────────────────────

func (r *AIConfigRepository) CreateAlert(alert *models.AIMonitorAlert) error {
	return basemodel.Add(r.db, alert)
}

func (r *AIConfigRepository) ListAlerts(userID string, limit int, onlyUnresolved bool) ([]models.AIMonitorAlert, error) {
	basemodel.EnsureSynced[models.AIMonitorAlert](r.db)
	var alerts []models.AIMonitorAlert
	q := r.db.Where("user_id = ?", userID)
	if onlyUnresolved {
		q = q.Where("resolved = ?", false)
	}
	err := q.Order("created_at desc").Limit(limit).Find(&alerts).Error
	return alerts, err
}

func (r *AIConfigRepository) ResolveAlert(id, userID string) error {
	return basemodel.Update[models.AIMonitorAlert](r.db, id, map[string]interface{}{"resolved": true})
}

func (r *AIConfigRepository) AlertExistsForDeployment(deploymentID string) (bool, error) {
	basemodel.EnsureSynced[models.AIMonitorAlert](r.db)
	var count int64
	err := r.db.Model(&models.AIMonitorAlert{}).Where("deployment_id = ? AND resolved = ?", deploymentID, false).Count(&count).Error
	return count > 0, err
}

func (r *AIConfigRepository) ListUsersWithMonitoring() ([]string, error) {
	basemodel.EnsureSynced[models.AIConfig](r.db)
	var ids []string
	err := r.db.Model(&models.AIConfig{}).Where("monitoring_enabled = ?", true).Pluck("user_id", &ids).Error
	return ids, err
}

// ─── AI Token Usage / Rate Limiting ──────────────────────────────────────────

func (r *AIConfigRepository) GetOrCreateTodayUsage(userID string) (*models.AITokenUsage, error) {
	basemodel.EnsureSynced[models.AITokenUsage](r.db)
	today := time.Now().UTC().Format("2006-01-02")
	var usage models.AITokenUsage

	err := r.db.Where("user_id = ? AND date = ?", userID, today).First(&usage).Error
	if err == nil {
		return &usage, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	usage = models.AITokenUsage{
		BaseModel: basemodel.BaseModel{ID: uuid.New().String()},
		UserID:    userID,
		Date:      today,
		Calls:     0,
		Tokens:    0,
	}

	// Use clauses for IGNORE equivalent gracefully across DBs
	err = r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&usage).Error
	if err != nil {
		return nil, err
	}

	err = r.db.Where("user_id = ? AND date = ?", userID, today).First(&usage).Error
	return &usage, err
}

func (r *AIConfigRepository) IncrementTodayUsage(userID string, deltaCalls int) error {
	usage, err := r.GetOrCreateTodayUsage(userID)
	if err != nil {
		return err
	}

	return r.db.Model(usage).UpdateColumn("calls", gorm.Expr("calls + ?", deltaCalls)).Error
}
