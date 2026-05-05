package services

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/vikukumar/pushpaka/internal/config"
	"github.com/vikukumar/pushpaka/internal/repositories"
	"github.com/vikukumar/pushpaka/pkg/models"
)

type ReplicationWorker struct {
	cfg          *config.Config
	registryRepo *repositories.RegistryManagementRepository
	logger       *zerolog.Logger
	ctx          context.Context
	cancel       context.CancelFunc

	activeSyncs sync.Map // repoID -> cancelFunc
}

func NewReplicationWorker(cfg *config.Config, registryRepo *repositories.RegistryManagementRepository, logger *zerolog.Logger) *ReplicationWorker {
	return &ReplicationWorker{
		cfg:          cfg,
		registryRepo: registryRepo,
		logger:       logger,
	}
}

func (w *ReplicationWorker) Start(ctx context.Context) {
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.logger.Info().Msg("Registry Replication Worker started")

	go w.run()
}

func (w *ReplicationWorker) run() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.checkPendingReplications()
		}
	}
}

func (w *ReplicationWorker) checkPendingReplications() {
	// Find replications that are due or failed
	reps, err := w.registryRepo.ListPendingReplications()
	if err != nil {
		return
	}

	for _, rep := range reps {
		if _, active := w.activeSyncs.Load(rep.RepoID); active {
			continue
		}

		go w.syncRepo(rep)
	}
}

func (w *ReplicationWorker) syncRepo(rep models.RegistryReplication) {
	ctx, cancel := context.WithCancel(w.ctx)
	w.activeSyncs.Store(rep.RepoID, cancel)
	defer w.activeSyncs.Delete(rep.RepoID)

	w.logger.Info().Str("repo_id", rep.RepoID).Msg("Starting replication sync")
	w.registryRepo.UpdateReplicationStatus(rep.ID, "syncing", "")

	// Get repo details to know the type
	repo, _ := w.registryRepo.GetRepo(rep.RepoID)

	var err error
	switch repo.Type {
	case models.RegistryTypeDocker, models.RegistryTypeHelm:
		err = w.syncOCI(ctx, rep, repo)
	case models.RegistryTypeBinary:
		err = w.syncBinary(ctx, rep, repo)
	}

	if err != nil {
		w.logger.Error().Err(err).Str("repo_id", rep.RepoID).Msg("Replication failed")
		w.registryRepo.UpdateReplicationStatus(rep.ID, "failed", err.Error())
	} else {
		w.logger.Info().Str("repo_id", rep.RepoID).Msg("Replication successful")
		w.registryRepo.UpdateReplicationStatus(rep.ID, "idle", "")
	}
}

func (w *ReplicationWorker) syncOCI(ctx context.Context, rep models.RegistryReplication, repo *models.RegistryRepo) error {
	// Use skopeo copy for high-performance multi-process blob transfer
	// destination format: docker://localhost:8080/registry/oci/<project>/<name>

	dest := fmt.Sprintf("docker://localhost:%s/registry/oci/%s/%s", w.cfg.Port, repo.ProjectID, repo.Name)

	// skopeo copy --all --dest-tls-verify=false docker://source docker://dest
	cmd := exec.CommandContext(ctx, "skopeo", "copy", "--all", "--dest-tls-verify=false", rep.SourceURL, dest)

	// TODO: Add credentials if source is private

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("skopeo failed: %w\nOutput: %s", err, string(out))
	}
	return nil
}

func (w *ReplicationWorker) syncBinary(ctx context.Context, rep models.RegistryReplication, repo *models.RegistryRepo) error {
	// For binaries, we could use a multi-process download tool like 'aria2c'
	// or implement parallel downloading in Go.

	// Using aria2c for fast multi-connection download
	// aria2c -x 16 -s 16 -d <dir> <url>

	destDir := fmt.Sprintf("%s/binaries/%s/%s", w.cfg.RegistryDir, repo.ProjectID, repo.Name)
	cmd := exec.CommandContext(ctx, "aria2c", "-x", "16", "-s", "16", "-d", destDir, "--allow-overwrite=true", rep.SourceURL)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("aria2c failed: %w\nOutput: %s", err, string(out))
	}
	return nil
}
