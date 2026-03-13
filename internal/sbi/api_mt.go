package sbi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/free5gc/amf/internal/logger"
	"github.com/free5gc/openapi"
	"github.com/free5gc/openapi/models"
	"github.com/free5gc/util/metrics/sbi"
)

func (s *Server) getMTRoutes() []Route {
	return []Route{
		{
			Method:  http.MethodGet,
			Pattern: "/",
			APIFunc: func(c *gin.Context) {
				c.String(http.StatusOK, "Hello World!")
			},
		},
		{
			Name:    "ProvideDomainSelectionInfo",
			Method:  http.MethodGet,
			Pattern: "/ue-contexts/:ueContextId",
			APIFunc: s.HTTPProvideDomainSelectionInfo,
		},
		{
			Name:    "EnableUeReachability",
			Method:  http.MethodPut,
			Pattern: "/ue-contexts/:ueContextId/ue-reachind",
			APIFunc: s.HTTPEnableUeReachability,
		},
		{
			Name:    "EnableGroupReachability",
			Method:  http.MethodPost,
			Pattern: "/ue-contexts/enable-group-reachability",
			APIFunc: s.HTTPEnableGroupReachability,
		},
	}
}

// ProvideDomainSelectionInfo - Namf_MT Provide Domain Selection Info service Operation
func (s *Server) HTTPProvideDomainSelectionInfo(c *gin.Context) {
	s.Processor().HandleProvideDomainSelectionInfoRequest(c)
}

func (s *Server) HTTPEnableUeReachability(c *gin.Context) {
	var reqData models.EnableUeReachabilityReqData

	requestBody, err := c.GetRawData()
	if err != nil {
		problemDetail := models.ProblemDetails{
			Title:  "System failure",
			Status: http.StatusInternalServerError,
			Detail: err.Error(),
			Cause:  "SYSTEM_FAILURE",
		}
		logger.MtLog.Errorf("Get Request Body error: %+v", err)
		c.Set(sbi.IN_PB_DETAILS_CTX_STR, problemDetail.Cause)
		c.JSON(http.StatusInternalServerError, problemDetail)
		return
	}

	if err := openapi.Deserialize(&reqData, requestBody, applicationjson); err != nil {
		problemDetail := reqbody + err.Error()
		rsp := models.ProblemDetails{
			Title:  "Malformed request syntax",
			Status: http.StatusBadRequest,
			Detail: problemDetail,
		}
		logger.MtLog.Errorln(problemDetail)
		c.Set(sbi.IN_PB_DETAILS_CTX_STR, http.StatusText(http.StatusBadRequest))
		c.JSON(http.StatusBadRequest, rsp)
		return
	}

	s.Processor().HandleEnableUeReachabilityRequest(c, reqData)
}

func (s *Server) HTTPEnableGroupReachability(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{})
}
