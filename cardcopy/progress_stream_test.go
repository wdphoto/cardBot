package cardcopy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func TestStreamSamplesAreRateLimited(t *testing.T) {
	// One-byte reads separated by a simulated quarter second. Callbacks
	// should report the 4th, 8th and 12th byte, not every read.
	at := time.Unix(0, 0)
	now := func() time.Time { at = at.Add(250 * time.Millisecond); return at }
	var samples []int64
	report := sampleReads(at, now, func(_ time.Time, n int64) { samples = append(samples, n) })
	n, err := copyStream(context.Background(), io.Discard, iotest.OneByteReader(strings.NewReader("1234567890123")), make([]byte, 32), report)
	if err != nil || n != 13 || !slices.Equal(samples, []int64{4, 8, 12}) {
		t.Fatalf("copied=%d samples=%v err=%v", n, samples, err)
	}
}

func TestExecute_SamplesBeforeCommit(t *testing.T) {
	for _, cancelEarly := range []bool{false, true} {
		name := "finish"
		if cancelEarly {
			name = "cancel"
		}
		t.Run(name, func(t *testing.T) {
			data := bytes.Repeat([]byte("movie"), 100_000)
			card := createTestCard(t, map[string]testFileSpec{"A.MOV": {data: data}})
			plan, err := PlanCopy(context.Background(), Options{CardPath: card, DestBase: t.TempDir(), BufferKB: 1, VerifyMode: "full"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			at := time.Unix(0, 0)
			now := func() time.Time { at = at.Add(time.Second); return at }
			var last Progress
			mid := 0
			result, err := execute(ctx, plan, func(p Progress) {
				if p.BytesDone+p.BytesInFlight < last.BytesDone+last.BytesInFlight || p.BytesDone+p.BytesInFlight > plan.TotalBytes {
					t.Fatalf("bad byte progress: %+v after %+v", p, last)
				}
				last = p
				if p.BytesInFlight == 0 {
					return
				}
				if p.FilesDone != 0 || p.BytesDone != 0 || p.CurrentFile == "" {
					t.Fatalf("credited partial: %+v", p)
				}
				if _, err := os.Stat(plan.Files[0].DestPath); !os.IsNotExist(err) {
					t.Fatalf("premature final: %v", err)
				}
				mid++
				if cancelEarly {
					cancel()
				}
			}, now)
			if mid == 0 {
				t.Fatal("no mid-file update")
			}
			if cancelEarly {
				if !errors.Is(err, context.Canceled) || result.FilesCopied != 0 || result.BytesCopied != 0 {
					t.Fatalf("cancel: %+v %v", result, err)
				}
				for _, path := range []string{plan.Files[0].DestPath, plan.Files[0].DestPath + ".part"} {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatalf("left %s: %v", path, err)
					}
				}
			} else if err != nil || mid < 2 || last.BytesInFlight != 0 || last.FilesDone != 1 || last.BytesDone != plan.TotalBytes || result.BytesCopied != plan.TotalBytes {
				t.Fatalf("finish: samples=%d last=%+v result=%+v err=%v", mid, last, result, err)
			}
		})
	}
}
