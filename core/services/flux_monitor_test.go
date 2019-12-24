package services_test

import (
	"chainlink/core/internal/cltest"
	"chainlink/core/internal/mocks"
	"chainlink/core/services"
	"chainlink/core/store/models"
	"chainlink/core/utils"
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func fakeSubscription() *mocks.Subscription {
	sub := new(mocks.Subscription)
	sub.On("Unsubscribe").Return()
	sub.On("Err").Return(nil)
	return sub
}

func TestConcreteFluxMonitor_AddJobRemoveJobHappy(t *testing.T) {
	store, cleanup := cltest.NewStore(t)
	defer cleanup()

	job := cltest.NewJobWithFluxMonitorInitiator()
	runManager := new(mocks.RunManager)
	started := make(chan struct{})

	dc := new(mocks.DeviationChecker)
	dc.On("Initialize", mock.Anything).Return(nil)
	dc.On("Start", mock.Anything).Return().Run(func(mock.Arguments) {
		started <- struct{}{}
	})

	checkerFactory := new(mocks.DeviationCheckerFactory)
	checkerFactory.On("New", job.Initiators[0], runManager).Return(dc, nil)
	fm := services.NewFluxMonitor(store, runManager)
	services.ExportedSetCheckerFactory(fm, checkerFactory)
	require.NoError(t, fm.Start())
	defer fm.Stop()
	require.NoError(t, fm.Connect(nil))
	defer fm.Disconnect()

	// 1. Add Job
	require.NoError(t, fm.AddJob(job))

	cltest.CallbackOrTimeout(t, "deviation checker started", func() {
		<-started
	})
	checkerFactory.AssertExpectations(t)
	dc.AssertExpectations(t)

	// 2. Remove Job
	removed := make(chan struct{})
	dc.On("Stop").Return().Run(func(mock.Arguments) {
		removed <- struct{}{}
	})
	fm.RemoveJob(job.ID)
	cltest.CallbackOrTimeout(t, "deviation checker stopped", func() {
		<-removed
	})
	dc.AssertExpectations(t)
}

func TestConcreteFluxMonitor_AddJobError(t *testing.T) {
	store, cleanup := cltest.NewStore(t)
	defer cleanup()

	job := cltest.NewJobWithFluxMonitorInitiator()
	runManager := new(mocks.RunManager)
	dc := new(mocks.DeviationChecker)
	dc.On("Initialize", mock.Anything).Return(errors.New("deliberate test error"))
	checkerFactory := new(mocks.DeviationCheckerFactory)
	checkerFactory.On("New", job.Initiators[0], runManager).Return(dc, nil)
	fm := services.NewFluxMonitor(store, runManager)
	services.ExportedSetCheckerFactory(fm, checkerFactory)
	require.NoError(t, fm.Start())
	defer fm.Stop()
	require.NoError(t, fm.Connect(nil))
	defer fm.Disconnect()

	require.Error(t, fm.AddJob(job))
	checkerFactory.AssertExpectations(t)
	dc.AssertExpectations(t)
}

func TestConcreteFluxMonitor_AddJobDisconnected(t *testing.T) {
	store, cleanup := cltest.NewStore(t)
	defer cleanup()

	job := cltest.NewJobWithFluxMonitorInitiator()
	runManager := new(mocks.RunManager)
	checkerFactory := new(mocks.DeviationCheckerFactory)
	dc := new(mocks.DeviationChecker)
	checkerFactory.On("New", job.Initiators[0], runManager).Return(dc, nil)
	fm := services.NewFluxMonitor(store, runManager)
	services.ExportedSetCheckerFactory(fm, checkerFactory)
	require.NoError(t, fm.Start())
	defer fm.Stop()

	require.NoError(t, fm.AddJob(job))
}

func TestConcreteFluxMonitor_AddJobNonFluxMonitor(t *testing.T) {
	store, cleanup := cltest.NewStore(t)
	defer cleanup()

	job := cltest.NewJobWithRunLogInitiator()
	runManager := new(mocks.RunManager)
	checkerFactory := new(mocks.DeviationCheckerFactory)
	fm := services.NewFluxMonitor(store, runManager)
	services.ExportedSetCheckerFactory(fm, checkerFactory)
	require.NoError(t, fm.Start())
	defer fm.Stop()

	require.NoError(t, fm.AddJob(job))
}

func TestConcreteFluxMonitor_ConnectStartsExistingJobs(t *testing.T) {
	store, cleanup := cltest.NewStore(t)
	defer cleanup()

	job := cltest.NewJobWithFluxMonitorInitiator()
	require.NoError(t, store.CreateJob(&job))

	job, err := store.FindJob(job.ID) // Update job from db to get matching coerced time values (UpdatedAt)
	require.NoError(t, err)

	runManager := new(mocks.RunManager)
	started := make(chan struct{})

	dc := new(mocks.DeviationChecker)
	dc.On("Initialize", mock.Anything).Return(nil)
	dc.On("Start", mock.Anything).Return().Run(func(mock.Arguments) {
		started <- struct{}{}
	})

	checkerFactory := new(mocks.DeviationCheckerFactory)
	checkerFactory.On("New", job.Initiators[0], runManager).Return(dc, nil)
	fm := services.NewFluxMonitor(store, runManager)
	services.ExportedSetCheckerFactory(fm, checkerFactory)
	require.NoError(t, fm.Start())
	defer fm.Stop()
	require.NoError(t, fm.Connect(nil))
	cltest.CallbackOrTimeout(t, "deviation checker started", func() {
		<-started
	})
	checkerFactory.AssertExpectations(t)
	dc.AssertExpectations(t)
}

func TestConcreteFluxMonitor_StopWithoutStart(t *testing.T) {
	store, cleanup := cltest.NewStore(t)
	defer cleanup()

	runManager := new(mocks.RunManager)

	fm := services.NewFluxMonitor(store, runManager)
	fm.Stop()
}

func TestPollingDeviationChecker_PollHappy(t *testing.T) {
	fetcher := new(mocks.Fetcher)
	fetcher.On("Fetch").Return(decimal.NewFromInt(102), nil)

	job := cltest.NewJobWithFluxMonitorInitiator()
	initr := job.Initiators[0]
	initr.ID = 1

	rm := new(mocks.RunManager)
	run := cltest.NewJobRun(job)
	data, err := models.ParseJSON([]byte(fmt.Sprintf(`{
			"result": "102",
			"address": "%s",
			"functionSelector": "0xe6330cf7",
			"dataPrefix": "0x0000000000000000000000000000000000000000000000000000000000000002"
	}`, initr.InitiatorParams.Address.Hex())))
	require.NoError(t, err)
	rm.On("Create", job.ID, &initr, &data, mock.Anything, mock.Anything).
		Return(&run, nil)

	checker, err := services.NewPollingDeviationChecker(initr, rm, fetcher, time.Second)
	require.NoError(t, err)
	assert.Equal(t, decimal.NewFromInt(0), checker.CurrentPrice())
	assert.Equal(t, big.NewInt(0), checker.CurrentRound())

	ethClient := new(mocks.Client)
	ethClient.On("GetAggregatorPrice", initr.InitiatorParams.Address, initr.InitiatorParams.Precision).
		Return(decimal.NewFromInt(100), nil)
	ethClient.On("GetAggregatorRound", initr.InitiatorParams.Address).
		Return(big.NewInt(1), nil)
	ethClient.On("SubscribeToLogs", mock.Anything, mock.Anything).
		Return(fakeSubscription(), nil)

	require.NoError(t, checker.Initialize(ethClient)) // setup
	ethClient.AssertExpectations(t)
	assert.Equal(t, decimal.NewFromInt(100), checker.CurrentPrice())
	assert.Equal(t, big.NewInt(1), checker.CurrentRound())

	require.NoError(t, checker.Poll()) // main entry point

	fetcher.AssertExpectations(t)
	rm.AssertExpectations(t)
	assert.Equal(t, decimal.NewFromInt(102), checker.CurrentPrice())
	assert.Equal(t, big.NewInt(2), checker.CurrentRound())
}

func TestPollingDeviationChecker_InitializeError(t *testing.T) {
	rm := new(mocks.RunManager)
	job := cltest.NewJobWithFluxMonitorInitiator()
	initr := job.Initiators[0]
	initr.ID = 1

	ethClient := new(mocks.Client)
	ethClient.On("GetAggregatorPrice", initr.InitiatorParams.Address, initr.InitiatorParams.Precision).
		Return(decimal.NewFromInt(0), errors.New("deliberate test error"))

	checker, err := services.NewPollingDeviationChecker(initr, rm, nil, time.Second)
	require.NoError(t, err)
	require.Error(t, checker.Initialize(ethClient))
}

func TestPollingDeviationChecker_StartStop(t *testing.T) {
	// 1. Set up fetcher to mark when polled
	fetcher := new(mocks.Fetcher)
	started := make(chan struct{})
	fetcher.On("Fetch").Return(decimal.NewFromFloat(100.0), nil).Maybe().Run(func(mock.Arguments) {
		select {
		case started <- struct{}{}:
		default:
		}
	})
	// can be called an arbitrary # of times
	defer fetcher.AssertExpectations(t)

	// 2. Prepare initialization to 100, which matches external adapter, so no deviation
	job := cltest.NewJobWithFluxMonitorInitiator()
	initr := job.Initiators[0]
	initr.ID = 1

	ethClient := new(mocks.Client)
	ethClient.On("GetAggregatorPrice", initr.InitiatorParams.Address, initr.InitiatorParams.Precision).
		Return(decimal.NewFromInt(100), nil)
	ethClient.On("GetAggregatorRound", initr.InitiatorParams.Address).
		Return(big.NewInt(1), nil)
	ethClient.On("SubscribeToLogs", mock.Anything, mock.Anything).
		Return(fakeSubscription(), nil)

	// 3. Start() with no delay to speed up test and polling.
	rm := new(mocks.RunManager)
	checker, err := services.NewPollingDeviationChecker(initr, rm, fetcher, 0)
	require.NoError(t, err)
	require.NoError(t, checker.Initialize(ethClient))

	done := make(chan struct{})
	go func() {
		checker.Start(context.Background()) // Start() polling
		done <- struct{}{}
	}()

	cltest.CallbackOrTimeout(t, "Start() starts", func() {
		<-started
	})

	// 4. Stop stops
	checker.Stop()
	cltest.CallbackOrTimeout(t, "Stop() unblocks Start()", func() {
		<-done
	})
}

func TestPollingDeviationChecker_NoDeviationLoopsCanBeCanceled(t *testing.T) {
	// 1. Set up fetcher to mark when polled
	fetcher := new(mocks.Fetcher)
	polled := make(chan struct{})
	fetcher.On("Fetch").Return(decimal.NewFromFloat(100.0), nil).Run(func(mock.Arguments) {
		select { // don't block if test isn't listening
		case polled <- struct{}{}:
		default:
		}
	})
	defer fetcher.AssertExpectations(t)

	// 2. Prepare initialization to 100, which matches external adapter, so no deviation
	job := cltest.NewJobWithFluxMonitorInitiator()
	initr := job.Initiators[0]
	initr.ID = 1

	ethClient := new(mocks.Client)
	ethClient.On("GetAggregatorPrice", initr.InitiatorParams.Address, initr.InitiatorParams.Precision).
		Return(decimal.NewFromInt(100), nil)
	ethClient.On("GetAggregatorRound", initr.InitiatorParams.Address).
		Return(big.NewInt(1), nil)
	ethClient.On("SubscribeToLogs", mock.Anything, mock.Anything).
		Return(fakeSubscription(), nil)

	// 3. Start() with no delay to speed up test and polling.
	rm := new(mocks.RunManager)
	checker, err := services.NewPollingDeviationChecker(initr, rm, fetcher, 0)
	require.NoError(t, err)
	require.NoError(t, checker.Initialize(ethClient))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		checker.Start(ctx) // Start() polling and delaying until cancel() from parent ctx.
		done <- struct{}{}
	}()

	// 4. Check if Polled
	cltest.CallbackOrTimeout(t, "start repeatedly polls external adapter", func() {
		<-polled // launched at the beginning of Start before delay
		<-polled // need two hits
	})

	// 5. Cancel parent context and ensure Start() stops.
	cancel()
	cltest.CallbackOrTimeout(t, "Start() unblocks and is done", func() {
		<-done
	})
}

func TestPollingDeviationChecker_StopWithoutStart(t *testing.T) {
	rm := new(mocks.RunManager)
	job := cltest.NewJobWithFluxMonitorInitiator()
	initr := job.Initiators[0]
	initr.ID = 1

	checker, err := services.NewPollingDeviationChecker(initr, rm, nil, time.Second)
	require.NoError(t, err)
	checker.Stop()
}

func TestPollingDeviationChecker_RespondToNewRound(t *testing.T) {
	currentRound := int64(5)

	// 1. Set up fetcher for 100
	fetcher := new(mocks.Fetcher)
	fetcher.On("Fetch").Return(decimal.NewFromFloat(100.0), nil).Maybe()
	defer fetcher.AssertExpectations(t)

	// 2. Prepare on-chain initialization to 100, which matches external adapter,
	// so no deviation
	job := cltest.NewJobWithFluxMonitorInitiator()
	initr := job.Initiators[0]
	initr.ID = 1

	ethClient := new(mocks.Client)
	ethClient.On("GetAggregatorPrice", initr.InitiatorParams.Address, initr.InitiatorParams.Precision).
		Return(decimal.NewFromInt(100), nil)
	ethClient.On("GetAggregatorRound", initr.InitiatorParams.Address).
		Return(big.NewInt(currentRound), nil)
	ethClient.On("SubscribeToLogs", mock.Anything, mock.Anything).
		Return(fakeSubscription(), nil)

	// 3. Initialize
	rm := new(mocks.RunManager)
	checker, err := services.NewPollingDeviationChecker(initr, rm, fetcher, time.Minute)
	require.NoError(t, err)
	require.NoError(t, checker.Initialize(ethClient))
	ethClient.AssertExpectations(t)

	// 5. Send rounds less than or equal to current, sequentially
	tests := []struct {
		name  string
		round uint64
	}{
		{"less than", 4},
		{"equal", 5},
	}

	for _, test := range tests {
		log := cltest.LogFromFixture(t, "testdata/new_round_log.json")
		log.Topics[models.NewRoundTopicRoundID] = common.BytesToHash(utils.EVMWordUint64(test.round))
		require.NoError(t, checker.RespondToNewRound(log))
		rm.AssertNotCalled(t, "Create", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	}

	// 6. Send log greater than current
	data, err := models.ParseJSON([]byte(fmt.Sprintf(`{
			"result": "100",
			"address": "%s",
			"functionSelector": "0xe6330cf7",
			"dataPrefix": "0x0000000000000000000000000000000000000000000000000000000000000006"
	}`, initr.InitiatorParams.Address.Hex()))) // dataPrefix has currentRound + 1
	require.NoError(t, err)

	rm.On("Create", mock.Anything, mock.Anything, &data, mock.Anything, mock.Anything).
		Return(nil, nil) // only round 6 triggers run.

	log := cltest.LogFromFixture(t, "testdata/new_round_log.json")
	log.Topics[models.NewRoundTopicRoundID] = common.BytesToHash(utils.EVMWordUint64(6))
	require.NoError(t, checker.RespondToNewRound(log))
	rm.AssertExpectations(t)
}

func TestOutsideDeviation(t *testing.T) {
	tests := []struct {
		name                string
		curPrice, nextPrice decimal.Decimal
		threshold           float64 // in percentage
		expectation         bool
	}{
		{"0 current price", decimal.NewFromInt(0), decimal.NewFromInt(100), 2, true},
		{"inside deviation", decimal.NewFromInt(100), decimal.NewFromInt(101), 2, false},
		{"equal to deviation", decimal.NewFromInt(100), decimal.NewFromInt(102), 2, true},
		{"outside deviation", decimal.NewFromInt(100), decimal.NewFromInt(103), 2, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := services.OutsideDeviation(test.curPrice, test.nextPrice, test.threshold)
			assert.Equal(t, test.expectation, actual)
		})
	}
}
