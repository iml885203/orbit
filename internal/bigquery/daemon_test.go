package bigquery

import (
	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/extension"
	"github.com/iml885203/orbit/internal/process"
)

type testHost struct{ cfg *config.Config }

func (h *testHost) Config() *config.Config                              { return h.cfg }
func (h *testHost) UpdateConfig(_ func(extension.ConfigTx) error) error { return nil }
func (h *testHost) ProcessMgr() *process.Manager                        { return nil }
