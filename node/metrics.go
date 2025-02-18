package node

import (
	"math"
	"time"

	"github.com/NethermindEth/juno/blockchain"
	"github.com/NethermindEth/juno/core"
	"github.com/NethermindEth/juno/db"
	"github.com/NethermindEth/juno/jsonrpc"
	"github.com/NethermindEth/juno/l1"
	"github.com/NethermindEth/juno/sync"
	"github.com/prometheus/client_golang/prometheus"
)

func makeDBMetrics() db.EventListener {
	latencyBuckets := []float64{
		25,
		50,
		75,
		100,
		250,
		500,
		1000, // 1ms
		2000,
		3000,
		4000,
		5000,
		10000,
		50000,
		500000,
		math.Inf(0),
	}
	readLatencyHistogram := prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "db",
		Name:      "read_latency",
		Buckets:   latencyBuckets,
	})
	writeLatencyHistogram := prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "db",
		Name:      "write_latency",
		Buckets:   latencyBuckets,
	})
	commitLatency := prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "db",
		Name:      "commit_latency",
		Buckets: []float64{
			5000,
			10000,
			20000,
			30000,
			40000,
			50000,
			100000, // 100ms
			200000,
			300000,
			500000,
			1000000,
			math.Inf(0),
		},
	})

	prometheus.MustRegister(readLatencyHistogram, writeLatencyHistogram, commitLatency)
	return &db.SelectiveListener{
		OnIOCb: func(write bool, duration time.Duration) {
			if write {
				writeLatencyHistogram.Observe(float64(duration.Microseconds()))
			} else {
				readLatencyHistogram.Observe(float64(duration.Microseconds()))
			}
		},
		OnCommitCb: func(duration time.Duration) {
			commitLatency.Observe(float64(duration.Microseconds()))
		},
	}
}

func makeHTTPMetrics() jsonrpc.NewRequestListener {
	reqCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "rpc",
		Subsystem: "http",
		Name:      "requests",
	})
	prometheus.MustRegister(reqCounter)

	return &jsonrpc.SelectiveListener{
		OnNewRequestCb: func(method string) {
			reqCounter.Inc()
		},
	}
}

func makeWSMetrics() jsonrpc.NewRequestListener {
	reqCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "rpc",
		Subsystem: "ws",
		Name:      "requests",
	})
	prometheus.MustRegister(reqCounter)

	return &jsonrpc.SelectiveListener{
		OnNewRequestCb: func(method string) {
			reqCounter.Inc()
		},
	}
}

func makeRPCMetrics(version, legacyVersion string) (jsonrpc.EventListener, jsonrpc.EventListener) {
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "rpc",
		Subsystem: "server",
		Name:      "requests",
	}, []string{"method", "version"})
	failedRequests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "rpc",
		Subsystem: "server",
		Name:      "failed_requests",
	}, []string{"method", "version"})
	requestLatencies := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "rpc",
		Subsystem: "server",
		Name:      "requests_latency",
	}, []string{"method", "version"})
	prometheus.MustRegister(requests, failedRequests, requestLatencies)

	return &jsonrpc.SelectiveListener{
			OnNewRequestCb: func(method string) {
				requests.WithLabelValues(method, version).Inc()
			},
			OnRequestHandledCb: func(method string, took time.Duration) {
				requestLatencies.WithLabelValues(method, version).Observe(took.Seconds())
			},
			OnRequestFailedCb: func(method string, data any) {
				failedRequests.WithLabelValues(method, version).Inc()
			},
		}, &jsonrpc.SelectiveListener{
			OnNewRequestCb: func(method string) {
				requests.WithLabelValues(method, legacyVersion).Inc()
			},
			OnRequestHandledCb: func(method string, took time.Duration) {
				requestLatencies.WithLabelValues(method, legacyVersion).Observe(took.Seconds())
			},
			OnRequestFailedCb: func(method string, data any) {
				failedRequests.WithLabelValues(method, legacyVersion).Inc()
			},
		}
}

func makeSyncMetrics(syncReader sync.Reader, bcReader blockchain.Reader) sync.EventListener {
	opTimerHistogram := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "sync",
		Name:      "timers",
	}, []string{"op"})
	blockCount := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "sync",
		Name:      "blocks",
	})
	reorgCount := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "sync",
		Name:      "reorganisations",
	})
	chainHeightGauge := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: "sync",
		Name:      "blockchain_height",
	}, func() float64 {
		height, _ := bcReader.Height()
		return float64(height)
	})
	bestBlockGauge := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: "sync",
		Name:      "best_known_block_number",
	}, func() float64 {
		bestHeader := syncReader.HighestBlockHeader()
		if bestHeader != nil {
			return float64(bestHeader.Number)
		}
		return 0
	})

	prometheus.MustRegister(opTimerHistogram, blockCount, chainHeightGauge, bestBlockGauge, reorgCount)

	return &sync.SelectiveListener{
		OnSyncStepDoneCb: func(op string, blockNum uint64, took time.Duration) {
			opTimerHistogram.WithLabelValues(op).Observe(took.Seconds())
			if op == sync.OpStore {
				blockCount.Inc()
			}
		},
		OnReorgCb: func(blockNum uint64) {
			reorgCount.Inc()
		},
	}
}

func makeJunoMetrics(version string) {
	prometheus.MustRegister(prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace:   "juno",
		Name:        "info",
		Help:        "Information about the Juno binary",
		ConstLabels: prometheus.Labels{"version": version},
	}))
}

func makeBlockchainMetrics() blockchain.EventListener {
	reads := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "blockchain",
		Name:      "reads",
	}, []string{"method"})
	prometheus.MustRegister(reads)

	return &blockchain.SelectiveListener{
		OnReadCb: func(method string) {
			reads.WithLabelValues(method).Inc()
		},
	}
}

func makeL1Metrics() l1.EventListener {
	l1Height := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "l1",
		Name:      "height",
	})
	prometheus.MustRegister(l1Height)

	return l1.SelectiveListener{
		OnNewL1HeadCb: func(head *core.L1Head) {
			l1Height.Set(float64(head.BlockNumber))
		},
	}
}
