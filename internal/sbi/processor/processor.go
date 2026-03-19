package processor

import (
	"github.com/acore/amf/internal/sbi/consumer"
	"github.com/acore/amf/pkg/app"
)

type ProcessorAmf interface {
	app.App

	Consumer() *consumer.Consumer
}

type Processor struct {
	ProcessorAmf
}

func NewProcessor(amf ProcessorAmf) (*Processor, error) {
	p := &Processor{
		ProcessorAmf: amf,
	}
	return p, nil
}
