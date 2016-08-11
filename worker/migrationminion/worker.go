// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package migrationminion

import (
	"github.com/juju/errors"
	"github.com/juju/loggo"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/core/migration"
	"github.com/juju/juju/network"
	"github.com/juju/juju/watcher"
	"github.com/juju/juju/worker"
	"github.com/juju/juju/worker/catacomb"
	"github.com/juju/juju/worker/fortress"
)

var logger = loggo.GetLogger("juju.worker.migrationminion")

// Facade exposes controller functionality to a Worker.
type Facade interface {
	Watch() (watcher.MigrationStatusWatcher, error)
	Report(migrationId string, phase migration.Phase, success bool) error
}

// Config defines the operation of a Worker.
type Config struct {
	Agent  agent.Agent
	Facade Facade
	Guard  fortress.Guard
}

// Validate returns an error if config cannot drive a Worker.
func (config Config) Validate() error {
	if config.Agent == nil {
		return errors.NotValidf("nil Agent")
	}
	if config.Facade == nil {
		return errors.NotValidf("nil Facade")
	}
	if config.Guard == nil {
		return errors.NotValidf("nil Guard")
	}
	return nil
}

// New returns a Worker backed by config, or an error.
func New(config Config) (worker.Worker, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Trace(err)
	}
	w := &Worker{config: config}
	err := catacomb.Invoke(catacomb.Plan{
		Site: &w.catacomb,
		Work: w.loop,
	})
	if err != nil {
		return nil, errors.Trace(err)
	}
	return w, nil
}

// Worker waits for a model migration to be active, then locks down the
// configured fortress and implements the migration.
type Worker struct {
	catacomb catacomb.Catacomb
	config   Config
}

// Kill implements worker.Worker.
func (w *Worker) Kill() {
	w.catacomb.Kill(nil)
}

// Wait implements worker.Worker.
func (w *Worker) Wait() error {
	return w.catacomb.Wait()
}

func (w *Worker) loop() error {
	watcher, err := w.config.Facade.Watch()
	if err != nil {
		return errors.Annotate(err, "setting up watcher")
	}
	if err := w.catacomb.Add(watcher); err != nil {
		return errors.Trace(err)
	}

	for {
		select {
		case <-w.catacomb.Dying():
			return w.catacomb.ErrDying()
		case status, ok := <-watcher.Changes():
			if !ok {
				return errors.New("watcher channel closed")
			}
			if err := w.handle(status); err != nil {
				return errors.Trace(err)
			}
		}
	}
}

func (w *Worker) handle(status watcher.MigrationStatus) error {
	logger.Infof("migration phase is now: %s", status.Phase)

	if !status.Phase.IsRunning() {
		return w.config.Guard.Unlock()
	}

	err := w.config.Guard.Lockdown(w.catacomb.Dying())
	if errors.Cause(err) == fortress.ErrAborted {
		return w.catacomb.ErrDying()
	} else if err != nil {
		return errors.Trace(err)
	}

	switch status.Phase {
	case migration.SUCCESS:
		// Report first because the config update in doSUCCESS will
		// cause the API connection to drop. The SUCCESS phase is the
		// point of no return anyway.
		if err := w.report(status, true); err != nil {
			return errors.Trace(err)
		}
		if err = w.doSUCCESS(status); err != nil {
			return errors.Trace(err)
		}
	default:
		// The minion doesn't need to do anything for other
		// migration phases.
	}
	return errors.Trace(err)
}

func (w *Worker) doSUCCESS(status watcher.MigrationStatus) error {
	hps, err := apiAddrsToHostPorts(status.TargetAPIAddrs)
	if err != nil {
		return errors.Annotate(err, "converting API addresses")
	}
	err = w.config.Agent.ChangeConfig(func(conf agent.ConfigSetter) error {
		conf.SetAPIHostPorts(hps)
		conf.SetCACert(status.TargetCACert)
		return nil
	})
	return errors.Annotate(err, "setting agent config")
}

func (w *Worker) report(status watcher.MigrationStatus, success bool) error {
	err := w.config.Facade.Report(status.MigrationId, status.Phase, success)
	return errors.Annotate(err, "failed to report phase progress")
}

func apiAddrsToHostPorts(addrs []string) ([][]network.HostPort, error) {
	hps, err := network.ParseHostPorts(addrs...)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return [][]network.HostPort{hps}, nil
}
