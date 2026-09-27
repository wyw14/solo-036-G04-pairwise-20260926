package valve

import (
	"fmt"
	"github.com/wyw14/solo-036/internal/model"
)

func (c *Controller) IsolationProof(runID string, epoch int64) model.CommandReceipt {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.isolated[runID]
	return model.CommandReceipt{RunID: runID, Command: "isolation", Epoch: value, Accepted: ok && value == epoch}
}

// IsolationEpoch returns the recorded isolation epoch for the run, or zero
// when no isolation proof has been completed.
func (c *Controller) IsolationEpoch(runID string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.isolated[runID]
}
func (c *Controller) OpenBypass(runID string, epoch int64) error {
	c.mu.Lock()
	value, ok := c.isolated[runID]
	c.mu.Unlock()
	if !ok || value != epoch {
		return fmt.Errorf("launcher isolation incomplete")
	}
	if c.pressure.Current() != 0 {
		return fmt.Errorf("pressure remains")
	}
	return nil
}
