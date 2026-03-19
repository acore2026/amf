package app

import (
	amf_context "github.com/acore/amf/internal/context"
	"github.com/acore/amf/pkg/factory"
)

type App interface {
	SetLogEnable(enable bool)
	SetLogLevel(level string)
	SetReportCaller(reportCaller bool)

	Start()
	Terminate()

	Context() *amf_context.AMFContext
	Config() *factory.Config
}
