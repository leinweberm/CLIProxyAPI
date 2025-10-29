// Package metrics provides handlers for the metrics endpoints.
package metrics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

// Handler holds the dependencies for the metrics handlers.
type Handler struct {
	Stats *usage.RequestStatistics
}

// NewHandler creates a new metrics handler.
func NewHandler(stats *usage.RequestStatistics) *Handler {
	return &Handler{Stats: stats}
}

// MetricsResponse is the top-level struct for the metrics endpoint response.
type MetricsResponse struct {
	Totals     TotalsMetrics      `json:"totals"`
	ByModel    []ModelMetrics     `json:"by_model"`
	Timeseries []TimeseriesBucket `json:"timeseries"`
}

// TotalsMetrics holds the aggregated totals for the queried period.
type TotalsMetrics struct {
	Tokens   int64 `json:"tokens"`
	Requests int64 `json:"requests"`
}

// ModelMetrics holds the aggregated metrics for a specific model.
type ModelMetrics struct {
	Model    string `json:"model"`
	Tokens   int64  `json:"tokens"`
	Requests int64  `json:"requests"`
}

// TimeseriesBucket holds the aggregated metrics for a specific time bucket.
type TimeseriesBucket struct {
	BucketStart string `json:"bucket_start"` // ISO 8601 format
	Tokens      int64  `json:"tokens"`
	Requests    int64  `json:"requests"`
}

// GetMetrics is the handler for the /_qs/metrics endpoint.
func (h *Handler) GetMetrics(c *gin.Context) {
	fromStr := c.Query("from")
	toStr := c.Query("to")
	modelFilter := c.Query("model")

	var fromTime, toTime time.Time
	var err error

	// Default to last 24 hours if no time range is given
	if fromStr == "" && toStr == "" {
		toTime = time.Now()
		fromTime = toTime.Add(-24 * time.Hour)
	} else {
		if fromStr != "" {
			fromTime, err = time.Parse(time.RFC3339, fromStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'from' timestamp format"})
				return
			}
		}
		if toStr != "" {
			toTime, err = time.Parse(time.RFC3339, toStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'to' timestamp format"})
				return
			}
		}
	}

	snapshot := h.Stats.Snapshot()

	modelMetricsMap := make(map[string]*ModelMetrics)
	timeseriesMap := make(map[time.Time]*TimeseriesBucket)
	var totalTokens int64
	var totalRequests int64

	for _, apiSnapshot := range snapshot.APIs {
		for modelName, modelSnapshot := range apiSnapshot.Models {
			if modelFilter != "" && modelFilter != modelName {
				continue
			}

			for _, detail := range modelSnapshot.Details {
				if !fromTime.IsZero() && detail.Timestamp.Before(fromTime) {
					continue
				}
				if !toTime.IsZero() && detail.Timestamp.After(toTime) {
					continue
				}

				totalRequests++
				totalTokens += detail.Tokens.TotalTokens

				if _, ok := modelMetricsMap[modelName]; !ok {
					modelMetricsMap[modelName] = &ModelMetrics{Model: modelName}
				}
				modelMetricsMap[modelName].Requests++
				modelMetricsMap[modelName].Tokens += detail.Tokens.TotalTokens

				bucket := detail.Timestamp.Truncate(time.Hour)
				if _, ok := timeseriesMap[bucket]; !ok {
					timeseriesMap[bucket] = &TimeseriesBucket{BucketStart: bucket.Format(time.RFC3339)}
				}
				timeseriesMap[bucket].Requests++
				timeseriesMap[bucket].Tokens += detail.Tokens.TotalTokens
			}
		}
	}

	resp := MetricsResponse{
		Totals: TotalsMetrics{
			Tokens:   totalTokens,
			Requests: totalRequests,
		},
		ByModel:    make([]ModelMetrics, 0, len(modelMetricsMap)),
		Timeseries: make([]TimeseriesBucket, 0, len(timeseriesMap)),
	}

	for _, mm := range modelMetricsMap {
		resp.ByModel = append(resp.ByModel, *mm)
	}

	sort.Slice(resp.ByModel, func(i, j int) bool {
		return resp.ByModel[i].Model < resp.ByModel[j].Model
	})

	for _, tb := range timeseriesMap {
		resp.Timeseries = append(resp.Timeseries, *tb)
	}

	sort.Slice(resp.Timeseries, func(i, j int) bool {
		return resp.Timeseries[i].BucketStart < resp.Timeseries[j].BucketStart
	})

	if jsonData, err := json.MarshalIndent(resp, "", "  "); err == nil {
		fmt.Println(string(jsonData))
	}

	c.JSON(http.StatusOK, resp)
}
