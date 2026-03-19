package processor

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/acore2026/amf/internal/context"
	"github.com/acore2026/amf/internal/logger"
	ngap_message "github.com/acore2026/amf/internal/ngap/message"
	"github.com/acore2026/openapi/models"
	"github.com/acore2026/util/metrics/sbi"
)

func (p *Processor) HandleProvideDomainSelectionInfoRequest(c *gin.Context) {
	logger.MtLog.Info("Handle Provide Domain Selection Info Request")

	ueContextID := c.Param("ueContextId")
	infoClassQuery := c.Query("info-class")
	supportedFeaturesQuery := c.Query("supported-features")

	ueContextInfo, problemDetails := p.ProvideDomainSelectionInfoProcedure(ueContextID,
		infoClassQuery, supportedFeaturesQuery)
	if problemDetails != nil {
		c.Set(sbi.IN_PB_DETAILS_CTX_STR, problemDetails.Cause)
		c.JSON(int(problemDetails.Status), problemDetails)
	} else {
		c.JSON(http.StatusOK, ueContextInfo)
	}
}

func (p *Processor) ProvideDomainSelectionInfoProcedure(ueContextID string, infoClassQuery string,
	supportedFeaturesQuery string) (
	*models.UeContextInfo, *models.ProblemDetails,
) {
	amfSelf := context.GetSelf()

	ue, ok := amfSelf.AmfUeFindByUeContextID(ueContextID)
	if !ok {
		logger.CtxLog.Warnf("AmfUe Context[%s] not found", ueContextID)
		problemDetails := &models.ProblemDetails{
			Status: http.StatusNotFound,
			Cause:  "CONTEXT_NOT_FOUND",
		}
		return nil, problemDetails
	}

	ue.Lock.Lock()
	defer ue.Lock.Unlock()

	ueContextInfo := new(models.UeContextInfo)

	// TODO: Error Status 307, 403 in TS29.518 Table 6.3.3.3.3.1-3
	anType := ue.GetAnType()
	if anType != "" && infoClassQuery != "" {
		ranUe := ue.RanUe[anType]
		ueContextInfo.AccessType = anType
		ueContextInfo.LastActTime = ranUe.LastActTime
		ueContextInfo.RatType = ue.RatType
		ueContextInfo.SupportedFeatures = ranUe.SupportedFeatures
		ueContextInfo.SupportVoPS = ranUe.SupportVoPS
		ueContextInfo.SupportVoPSn3gpp = ranUe.SupportVoPSn3gpp
	}

	return ueContextInfo, nil
}

func (p *Processor) HandleEnableUeReachabilityRequest(c *gin.Context,
	reqData models.EnableUeReachabilityReqData,
) {
	logger.MtLog.Info("Handle Enable UE Reachability Request")

	ueContextID := c.Param("ueContextId")
	rspData, problemDetails := p.EnableUeReachabilityProcedure(ueContextID, reqData)
	if problemDetails != nil {
		c.Set(sbi.IN_PB_DETAILS_CTX_STR, problemDetails.Cause)
		c.JSON(int(problemDetails.Status), problemDetails)
		return
	}

	c.JSON(http.StatusOK, rspData)
}

func (p *Processor) EnableUeReachabilityProcedure(ueContextID string,
	reqData models.EnableUeReachabilityReqData,
) (*models.EnableUeReachabilityRspData, *models.ProblemDetailsEnableUeReachability) {
	amfSelf := context.GetSelf()

	ue, ok := amfSelf.AmfUeFindByUeContextID(ueContextID)
	if !ok {
		logger.CtxLog.Warnf("AmfUe Context[%s] not found", ueContextID)
		return nil, &models.ProblemDetailsEnableUeReachability{
			Status: http.StatusNotFound,
			Cause:  "CONTEXT_NOT_FOUND",
		}
	}

	ue.Lock.Lock()
	defer ue.Lock.Unlock()

	rspData := &models.EnableUeReachabilityRspData{
		Reachability:      successReachability(reqData.Reachability),
		SupportedFeatures: reqData.SupportedFeatures,
	}

	if ue.CmConnect(models.AccessType__3_GPP_ACCESS) || ue.CmConnect(models.AccessType_NON_3_GPP_ACCESS) {
		return rspData, nil
	}

	if !ue.State[models.AccessType__3_GPP_ACCESS].Is(context.Registered) {
		return nil, &models.ProblemDetailsEnableUeReachability{
			Status:         http.StatusGatewayTimeout,
			Cause:          "UE_NOT_REACHABLE",
			MaxWaitingTime: int32(amfSelf.T3513Cfg.ExpireTime / 1e9),
		}
	}

	ue.SetOnGoing(models.AccessType__3_GPP_ACCESS, &context.OnGoing{
		Procedure: context.OnGoingProcedurePaging,
	})

	pkg, err := ngap_message.BuildPaging(ue, nil, false)
	if err != nil {
		logger.MtLog.Errorf("Build Paging failed: %v", err)
		return nil, &models.ProblemDetailsEnableUeReachability{
			Status: http.StatusInternalServerError,
			Cause:  "SYSTEM_FAILURE",
			Detail: err.Error(),
		}
	}

	ngap_message.SendPaging(ue, pkg)

	return rspData, nil
}

func successReachability(requested models.UeReachability) models.UeReachability {
	if requested != "" {
		return requested
	}

	return models.UeReachability_REACHABLE
}
