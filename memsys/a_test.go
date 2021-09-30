// Package memsys provides memory management and Slab allocation
// with io.Reader and io.Writer interfaces on top of a scatter-gather lists
// (of reusable buffers)
/*
 * Copyright (c) 2018-2020, NVIDIA CORPORATION. All rights reserved.
 */
package memsys_test

// How to run:
//
// 1) run each of the tests for 2 minutes while redirecting glog to STDERR:
//
// go test -v -logtostderr=true -duration 2m
//
// 2) same as above with DEBUG and glog level = 4 (verbose):
//
// AIS_DEBUG=memsys=4 go test -v -logtostderr=true -duration 2m -tags=debug
//
// 3) run tests matching "No" with debug and glog level = 1 (non-verbose):
//
// AIS_DEBUG=memsys=1 go test -v -logtostderr=true -run=No -tags=debug
//
// 4) run each test for 10 minutes with the permission to use up to 90% of total RAM
//
// AIS_MINMEM_PCT_TOTAL=10 go test -v -run=No -duration 10m -timeout=1h
//
// 5) same, with debug, glog to STDERR and verbose output generated by the tests:
//
// AIS_MINMEM_PCT_TOTAL=10 go test -v -logtostderr=true -run=No -duration 10m -verbose true -tags=debug -timeout=1h

import (
	"flag"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/NVIDIA/aistore/cmn/cos"
	"github.com/NVIDIA/aistore/devtools/tlog"
	"github.com/NVIDIA/aistore/memsys"
)

var (
	duration time.Duration // test duration
	verbose  bool
)

func TestMain(t *testing.M) {
	var (
		d   string
		err error
	)
	flag.StringVar(&d, "duration", "30s", "test duration")
	flag.BoolVar(&verbose, "verbose", false, "verbose")
	flag.Parse()

	if duration, err = time.ParseDuration(d); err != nil {
		cos.Exitf("Invalid duration %q", d)
	}

	os.Exit(t.Run())
}

func Test_Sleep(t *testing.T) {
	if testing.Short() {
		duration = 4 * time.Second
	}

	mem := &memsys.MMSA{TimeIval: time.Second * 20, MinFree: cos.GiB, Name: "amem"}
	err := mem.Init(false /*panicOnErr*/)
	if err != nil {
		t.Fatal(err)
	}

	wg := &sync.WaitGroup{}
	random := cos.NowRand()
	for i := 0; i < 100; i++ {
		ttl := time.Duration(random.Int63n(int64(time.Millisecond*100))) + time.Millisecond
		var siz, tot int64
		if i%2 == 0 {
			siz = random.Int63n(cos.MiB*10) + cos.KiB
		} else {
			siz = random.Int63n(cos.KiB*100) + cos.KiB
		}
		tot = random.Int63n(cos.DivCeil(cos.MiB*50, siz))*siz + cos.KiB
		wg.Add(1)
		go memstress(mem, i, ttl, siz, tot, wg)
	}
	c := make(chan struct{}, 1)
	go printMaxRingLen(mem, c)
	for i := 0; i < 7; i++ {
		time.Sleep(duration / 8)
		mem.FreeSpec(memsys.FreeSpec{IdleDuration: 1, MinSize: cos.MiB})
	}
	wg.Wait()
	close(c)
	mem.Terminate()
}

func Test_NoSleep(t *testing.T) {
	if testing.Short() {
		duration = 4 * time.Second
	}

	mem := &memsys.MMSA{TimeIval: time.Second * 20, MinPctTotal: 5, Name: "bmem"}
	err := mem.Init(false /*panicOnErr*/)
	if err != nil {
		t.Fatal(err)
	}
	go printStats(mem)

	wg := &sync.WaitGroup{}
	random := cos.NowRand()
	for i := 0; i < 500; i++ {
		siz := random.Int63n(cos.MiB) + cos.KiB
		tot := random.Int63n(cos.DivCeil(cos.KiB*10, siz))*siz + cos.KiB
		wg.Add(1)
		go memstress(mem, i, time.Millisecond, siz, tot, wg)
	}
	c := make(chan struct{}, 1)
	go printMaxRingLen(mem, c)
	for i := 0; i < 7; i++ {
		time.Sleep(duration / 8)
		mem.FreeSpec(memsys.FreeSpec{Totally: true, ToOS: true, MinSize: cos.MiB * 10})
	}
	wg.Wait()
	close(c)
	mem.Terminate()
}

func printMaxRingLen(mem *memsys.MMSA, c chan struct{}) {
	for i := 0; i < 100; i++ {
		select {
		case <-c:
			return
		case <-time.After(5 * time.Second):
			if p, _ := mem.MemPressure(); p > memsys.MemPressureLow {
				tlog.Logf("%s\n", mem.MemPressure2S(p))
			}
		}
	}
}

func memstress(mem *memsys.MMSA, id int, ttl time.Duration, siz, tot int64, wg *sync.WaitGroup) {
	defer wg.Done()
	sgls := make([]*memsys.SGL, 0, 128)
	x := cos.B2S(siz, 1) + "/" + cos.B2S(tot, 1)
	if id%100 == 0 && verbose {
		if ttl > time.Millisecond {
			tlog.Logf("%4d: %-19s ttl %v\n", id, x, ttl)
		} else {
			tlog.Logf("%4d: %-19s\n", id, x)
		}
	}
	started := time.Now()
	for {
		t := tot
		for t > 0 {
			sgls = append(sgls, mem.NewSGL(siz))
			t -= siz
		}
		time.Sleep(ttl)
		for i, sgl := range sgls {
			sgl.Free()
			sgls[i] = nil
		}
		sgls = sgls[:0]
		if time.Since(started) > duration {
			break
		}
	}
	if id%100 == 0 && verbose {
		tlog.Logf("%4d: done\n", id)
	}
}

func printStats(mem *memsys.MMSA) {
	for {
		time.Sleep(mem.TimeIval)
		stats := mem.GetStats()
		for i := 0; i < memsys.NumPageSlabs; i++ {
			slab, err := mem.GetSlab(int64(i+1) * memsys.PageSize)
			cos.AssertNoErr(err)
			x := ""
			idle := stats.Idle[i]
			if idle > 0 {
				x = fmt.Sprintf(", idle=%v", idle)
			}
			fmt.Printf("%s: hits %d%s\n", slab.Tag(), stats.Hits[i], x)
		}
	}
}
