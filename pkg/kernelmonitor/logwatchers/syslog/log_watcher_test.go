/*
Copyright 2016 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package syslog

import (
	"io/ioutil"
	"os"
	"testing"
	"time"

	"k8s.io/node-problem-detector/pkg/kernelmonitor/logwatchers/types"
	kerntypes "k8s.io/node-problem-detector/pkg/kernelmonitor/types"

	"code.cloudfoundry.org/clock/fakeclock"
	"github.com/stretchr/testify/assert"
)

func TestWatch(t *testing.T) {
	// now is a fake time
	now := time.Date(time.Now().Year(), time.January, 2, 3, 4, 5, 0, time.Local)
	fakeClock := fakeclock.NewFakeClock(now)
	testCases := []struct {
		log      string
		logs     []kerntypes.KernelLog
		uptime   time.Time
		lookback string
	}{
		{
			// The start point is at the head of the log file.
			log: `Jan  2 03:04:05 kernel: [0.000000] 1
			Jan  2 03:04:06 kernel: [1.000000] 2
			Jan  2 03:04:07 kernel: [2.000000] 3
			`,
			lookback: "0",
			logs: []kerntypes.KernelLog{
				{
					Timestamp: now,
					Message:   "1",
				},
				{
					Timestamp: now.Add(time.Second),
					Message:   "2",
				},
				{
					Timestamp: now.Add(2 * time.Second),
					Message:   "3",
				},
			},
		},
		{
			// The start point is in the middle of the log file.
			log: `Jan  2 03:04:04 kernel: [0.000000] 1
			Jan  2 03:04:05 kernel: [1.000000] 2
			Jan  2 03:04:06 kernel: [2.000000] 3
			`,
			lookback: "0",
			logs: []kerntypes.KernelLog{
				{
					Timestamp: now,
					Message:   "2",
				},
				{
					Timestamp: now.Add(time.Second),
					Message:   "3",
				},
			},
		},
		{
			// The start point is at the end of the log file, but we look back.
			log: `Jan  2 03:04:03 kernel: [0.000000] 1
			Jan  2 03:04:04 kernel: [1.000000] 2
			Jan  2 03:04:05 kernel: [2.000000] 3
			`,
			lookback: "1s",
			logs: []kerntypes.KernelLog{
				{
					Timestamp: now.Add(-time.Second),
					Message:   "2",
				},
				{
					Timestamp: now,
					Message:   "3",
				},
			},
		},
		{
			// The start point is at the end of the log file, we look back, but
			// system rebooted at in the middle of the log file.
			log: `Jan  2 03:04:03 kernel: [0.000000] 1
			Jan  2 03:04:04 kernel: [1.000000] 2
			Jan  2 03:04:05 kernel: [2.000000] 3
			`,
			uptime:   time.Date(time.Now().Year(), time.January, 2, 3, 4, 4, 0, time.Local),
			lookback: "2s",
			logs: []kerntypes.KernelLog{
				{
					Timestamp: now.Add(-time.Second),
					Message:   "2",
				},
				{
					Timestamp: now,
					Message:   "3",
				},
			},
		},
	}
	for c, test := range testCases {
		t.Logf("TestCase #%d: %#v", c+1, test)
		f, err := ioutil.TempFile("", "kernel_log_watcher_test")
		assert.NoError(t, err)
		defer func() {
			f.Close()
			os.Remove(f.Name())
		}()
		_, err = f.Write([]byte(test.log))
		assert.NoError(t, err)

		w := NewSyslogWatcherOrDie(types.WatcherConfig{
			Plugin:   "syslog",
			LogPath:  f.Name(),
			Lookback: test.lookback,
		})
		// Set the uptime.
		w.(*syslogWatcher).uptime = test.uptime
		// Set the fake clock.
		w.(*syslogWatcher).clock = fakeClock
		logCh, err := w.Watch()
		assert.NoError(t, err)
		defer w.Stop()
		for _, expected := range test.logs {
			select {
			case got := <-logCh:
				assert.Equal(t, &expected, got)
			case <-time.After(30 * time.Second):
				t.Errorf("timeout waiting for log")
			}
		}
		// The log channel should have already been drained
		// There could stil be future messages sent into the channel, but the chance is really slim.
		timeout := time.After(100 * time.Millisecond)
		select {
		case log := <-logCh:
			t.Errorf("unexpected extra log: %+v", *log)
		case <-timeout:
		}
	}
}