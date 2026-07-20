package context

import (
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/acore2026/amf/internal/logger"
	"github.com/acore2026/ngap/ngapConvert"
	"github.com/acore2026/ngap/ngapType"
	"github.com/acore2026/openapi/models"
)

const (
	RanPresentGNbId   = 1
	RanPresentNgeNbId = 2
	RanPresentN3IwfId = 3
	RanPresentTngfId  = 4
	RanPresentTwifId  = 5
	RanPresentWagfId  = 6
)

type AmfRan struct {
	RanPresent int
	RanId      *models.GlobalRanNodeId
	Name       string
	AnType     models.AccessType
	/* socket Connect*/
	Conn net.Conn
	/* Supported TA List */
	SupportedTAList []SupportedTAI

	/* RAN UE List */
	RanUeList sync.Map // RanUeNgapId as key
	writeOnce sync.Once
	writeSlot chan struct{}

	/* logger */
	Log *logrus.Entry
}

func (ran *AmfRan) WritePacket(packet []byte, timeout time.Duration) (int, error) {
	if ran == nil || ran.Conn == nil {
		return 0, fmt.Errorf("RAN connection is nil")
	}
	ran.writeOnce.Do(func() {
		ran.writeSlot = make(chan struct{}, 1)
		ran.writeSlot <- struct{}{}
	})

	if timeout <= 0 {
		<-ran.writeSlot
		defer func() { ran.writeSlot <- struct{}{} }()
		return ran.Conn.Write(packet)
	}

	deadline := time.Now().Add(timeout)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ran.writeSlot:
	case <-timer.C:
		return 0, fmt.Errorf("wait for RAN writer: %w", timeoutError(timeout))
	}

	remaining := time.Until(deadline)
	if remaining <= 0 {
		ran.writeSlot <- struct{}{}
		return 0, timeoutError(timeout)
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(remaining)

	type writeResult struct {
		n   int
		err error
	}
	resultCh := make(chan writeResult, 1)
	conn := ran.Conn
	go func() {
		result := writeResult{}
		defer func() {
			if recovered := recover(); recovered != nil {
				result.n = 0
				result.err = fmt.Errorf("RAN connection write panic: %v", recovered)
			}
			ran.writeSlot <- struct{}{}
			resultCh <- result
		}()
		deadlineSet := conn.SetWriteDeadline(deadline) == nil
		result.n, result.err = conn.Write(packet)
		if deadlineSet {
			_ = conn.SetWriteDeadline(time.Time{})
		}
	}()

	select {
	case result := <-resultCh:
		return result.n, result.err
	case <-timer.C:
		select {
		case result := <-resultCh:
			return result.n, result.err
		default:
		}
		_ = conn.Close()
		return 0, timeoutError(timeout)
	}
}

func timeoutError(timeout time.Duration) error {
	return fmt.Errorf("RAN write timed out after %s: %w", timeout, os.ErrDeadlineExceeded)
}

type SupportedTAI struct {
	Tai        models.Tai
	SNssaiList []models.Snssai
}

func NewSupportedTAI() (tai SupportedTAI) {
	tai.SNssaiList = make([]models.Snssai, 0, MaxNumOfSlice)
	return
}

func (ran *AmfRan) Remove() {
	ran.Log.Infof("Remove RAN Context[ID: %+v]", ran.RanID())
	ran.RemoveAllRanUe(true)
	GetSelf().DeleteAmfRan(ran.Conn)
}

func (ran *AmfRan) NewRanUe(ranUeNgapID int64) (*RanUe, error) {
	ranUe := RanUe{}
	self := GetSelf()
	amfUeNgapID, err := self.AllocateAmfUeNgapID()
	if err != nil {
		return nil, fmt.Errorf("allocate AMF UE NGAP ID error: %+v", err)
	}
	ranUe.AmfUeNgapId = amfUeNgapID
	ranUe.RanUeNgapId = ranUeNgapID
	ranUe.Ran = ran
	ranUe.Log = ran.Log
	ranUe.HoldingAmfUe = nil
	ranUe.UpdateLogFields()

	if ranUeNgapID != RanUeNgapIdUnspecified {
		// store to RanUeList only when RANUENGAPID is specified
		// (otherwise, will be stored only in amfContext.RanUePool)
		ran.RanUeList.Store(ranUeNgapID, &ranUe)
	}
	self.RanUePool.Store(ranUe.AmfUeNgapId, &ranUe)
	ranUe.Log.Infof("New RanUe [RanUeNgapID:%d][AmfUeNgapID:%d]", ranUe.RanUeNgapId, ranUe.AmfUeNgapId)
	return &ranUe, nil
}

func (ran *AmfRan) RemoveAllRanUe(removeAmfUe bool) {
	// Using revered removal since ranUe.Remove() will also modify the slice r.RanUeList
	ran.RanUeList.Range(func(k, v interface{}) bool {
		ranUe := v.(*RanUe)
		if err := ranUe.Remove(); err != nil {
			logger.CtxLog.Errorf("Remove RanUe error: %v", err)
		}
		return true
	})
}

func (ran *AmfRan) RanUeFindByRanUeNgapID(ranUeNgapID int64) *RanUe {
	if value, ok := ran.RanUeList.Load(ranUeNgapID); ok {
		return value.(*RanUe)
	}
	return nil
}

func (ran *AmfRan) FindRanUeByAmfUeNgapID(amfUeNgapID int64) *RanUe {
	var ru *RanUe
	ran.RanUeList.Range(func(k, v interface{}) bool {
		ranUe := v.(*RanUe)
		if ranUe.AmfUeNgapId == amfUeNgapID {
			ru = ranUe
			return false
		}
		return true
	})
	return ru
}

func (ran *AmfRan) SetRanId(ranNodeId *ngapType.GlobalRANNodeID) {
	ranId := ngapConvert.RanIdToModels(*ranNodeId)
	ran.RanPresent = ranNodeId.Present
	ran.RanId = &ranId
	if ranNodeId.Present == ngapType.GlobalRANNodeIDPresentGlobalN3IWFID ||
		ranNodeId.Present == ngapType.GlobalRANNodeIDPresentChoiceExtensions {
		ran.AnType = models.AccessType_NON_3_GPP_ACCESS
	} else {
		ran.AnType = models.AccessType__3_GPP_ACCESS
	}
}

func (ran *AmfRan) RanID() string {
	switch ran.RanPresent {
	case RanPresentGNbId:
		return fmt.Sprintf("<PlmnID: %+v, GNbID: %s>", *ran.RanId.PlmnId, ran.RanId.GNbId.GNBValue)
	case RanPresentN3IwfId:
		return fmt.Sprintf("<PlmnID: %+v, N3IwfID: %s>", *ran.RanId.PlmnId, ran.RanId.N3IwfId)
	case RanPresentNgeNbId:
		return fmt.Sprintf("<PlmnID: %+v, NgeNbID: %s>", *ran.RanId.PlmnId, ran.RanId.NgeNbId)
	default:
		return ""
	}
}

func (ran *AmfRan) UeRatType() models.RatType {
	// In TS 23.501 5.3.2.3
	// For 3GPP access the AMF determines the RAT type the UE is camping on based
	// on the Global RAN Node IDs associated with the N2 interface and
	// additionally the Tracking Area indicated by NG-RAN
	switch ran.RanPresent {
	case RanPresentGNbId:
		return models.RatType_NR
	case RanPresentNgeNbId:
		return models.RatType_NR
	case RanPresentN3IwfId:
		return models.RatType_VIRTUAL
	case RanPresentTngfId:
		return models.RatType_TRUSTED_N3_GA
	case RanPresentTwifId:
		return models.RatType_TRUSTED_N3_GA
	case RanPresentWagfId:
		return models.RatType_WIRELINE
	default:
		return models.RatType_NR
	}
}
