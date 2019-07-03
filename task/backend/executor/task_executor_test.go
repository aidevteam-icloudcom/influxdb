package executor_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/influxdata/influxdb"
	platform "github.com/influxdata/influxdb"
	icontext "github.com/influxdata/influxdb/context"
	"github.com/influxdata/influxdb/inmem"
	"github.com/influxdata/influxdb/kv"
	"github.com/influxdata/influxdb/query"
	"github.com/influxdata/influxdb/task/backend/executor"
	"github.com/influxdata/influxdb/task/backend/scheduler"
	"go.uber.org/zap/zaptest"
)

type tes struct {
	svc *fakeQueryService
	ex  *executor.TaskExecutor
	i   *kv.Service
	tc  testCreds
}

func taskExecutorSystem(t *testing.T) tes {
	aqs := newFakeQueryService()
	qs := query.QueryServiceBridge{
		AsyncQueryService: aqs,
	}

	i := kv.NewService(inmem.NewKVStore())

	ex := executor.NewExecutor(zaptest.NewLogger(t), qs, i, i, i)
	return tes{
		svc: aqs,
		ex:  ex,
		i:   i,
		tc:  createCreds(t, i),
	}
}

func TestTaskExecutor(t *testing.T) {
	t.Run("QuerySuccess", testQuerySuccess)
	t.Run("QueryFailure", testQueryFailure)
	t.Run("ManualRun", testManualRun)
	t.Run("ResumeRun", testResumingRun)
}

func testQuerySuccess(t *testing.T) {
	t.Parallel()
	tes := taskExecutorSystem(t)

	script := fmt.Sprintf(fmtTestScript, t.Name())
	ctx := icontext.SetAuthorizer(context.Background(), tes.tc.Auth)
	task, err := tes.i.CreateTask(ctx, platform.TaskCreate{OrganizationID: tes.tc.OrgID, Token: tes.tc.Auth.Token, Flux: script})
	if err != nil {
		t.Fatal(err)
	}

	promise, err := tes.ex.Execute(ctx, scheduler.ID(task.ID), time.Unix(123, 0))
	if err != nil {
		t.Fatal(err)
	}
	promiseID := influxdb.ID(promise.ID())

	run, err := tes.i.FindRunByID(context.Background(), task.ID, promiseID)
	if err != nil {
		t.Fatal(err)
	}

	if run.ID != promiseID {
		t.Fatal("promise and run dont match")
	}

	tes.svc.WaitForQueryLive(t, script)
	tes.svc.SucceedQuery(script)

	<-promise.Done()

	if got := promise.Error(); got != nil {
		t.Fatal(got)
	}
}

func testQueryFailure(t *testing.T) {
	t.Parallel()
	tes := taskExecutorSystem(t)

	script := fmt.Sprintf(fmtTestScript, t.Name())
	ctx := icontext.SetAuthorizer(context.Background(), tes.tc.Auth)
	task, err := tes.i.CreateTask(ctx, platform.TaskCreate{OrganizationID: tes.tc.OrgID, Token: tes.tc.Auth.Token, Flux: script})
	if err != nil {
		t.Fatal(err)
	}

	promise, err := tes.ex.Execute(ctx, scheduler.ID(task.ID), time.Unix(123, 0))
	if err != nil {
		t.Fatal(err)
	}
	promiseID := influxdb.ID(promise.ID())

	run, err := tes.i.FindRunByID(context.Background(), task.ID, promiseID)
	if err != nil {
		t.Fatal(err)
	}

	if run.ID != promiseID {
		t.Fatal("promise and run dont match")
	}

	tes.svc.WaitForQueryLive(t, script)
	tes.svc.FailQuery(script, errors.New("blargyblargblarg"))

	<-promise.Done()

	if got := promise.Error(); got == nil {
		t.Fatal("got no error when I should have")
	}
}

func testManualRun(t *testing.T) {
	t.Parallel()
	tes := taskExecutorSystem(t)

	script := fmt.Sprintf(fmtTestScript, t.Name())
	ctx := icontext.SetAuthorizer(context.Background(), tes.tc.Auth)
	task, err := tes.i.CreateTask(ctx, platform.TaskCreate{OrganizationID: tes.tc.OrgID, Token: tes.tc.Auth.Token, Flux: script})
	if err != nil {
		t.Fatal(err)
	}

	manualRun, err := tes.i.ForceRun(ctx, task.ID, 123)
	if err != nil {
		t.Fatal(err)
	}

	mr, err := tes.i.ManualRuns(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(mr) != 1 {
		t.Fatal("manual run not created by force run")
	}

	promise, err := tes.ex.Execute(ctx, scheduler.ID(task.ID), time.Unix(123, 0))
	if err != nil {
		t.Fatal(err)
	}
	promiseID := influxdb.ID(promise.ID())

	run, err := tes.i.FindRunByID(context.Background(), task.ID, promiseID)
	if err != nil {
		t.Fatal(err)
	}

	if run.ID != promiseID || manualRun.ID != promiseID {
		t.Fatal("promise and run and manual run dont match")
	}

	tes.svc.WaitForQueryLive(t, script)
	tes.svc.SucceedQuery(script)

	if got := promise.Error(); got != nil {
		t.Fatal(got)
	}
}

func testResumingRun(t *testing.T) {
	t.Parallel()
	tes := taskExecutorSystem(t)

	script := fmt.Sprintf(fmtTestScript, t.Name())
	ctx := icontext.SetAuthorizer(context.Background(), tes.tc.Auth)
	task, err := tes.i.CreateTask(ctx, platform.TaskCreate{OrganizationID: tes.tc.OrgID, Token: tes.tc.Auth.Token, Flux: script})
	if err != nil {
		t.Fatal(err)
	}

	stalledRun, err := tes.i.CreateRun(ctx, task.ID, time.Unix(123, 0))
	if err != nil {
		t.Fatal(err)
	}

	promise, err := tes.ex.Execute(ctx, scheduler.ID(task.ID), time.Unix(123, 0))
	if err != nil {
		t.Fatal(err)
	}
	promiseID := influxdb.ID(promise.ID())

	run, err := tes.i.FindRunByID(context.Background(), task.ID, promiseID)
	if err != nil {
		t.Fatal(err)
	}

	if run.ID != promiseID || stalledRun.ID != promiseID {
		t.Fatal("promise and run and manual run dont match")
	}

	tes.svc.WaitForQueryLive(t, script)
	tes.svc.SucceedQuery(script)

	if got := promise.Error(); got != nil {
		t.Fatal(got)
	}
}
