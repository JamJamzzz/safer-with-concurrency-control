package client

import (
	"bytes"
	"sync"

	userlib "github.com/cs161-staff/project2-userlib"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Storage concurrency", func() {
	Specify("concurrent datastore reads preserve exact bandwidth accounting", func() {
		// No SAFER logical locks: even reads of independent keys share
		// userlib's bandwidth counter. Setup/reset and the final observation
		// occur only while workers are quiescent.
		userlib.DatastoreClear()
		defer userlib.DatastoreClear()
		const workers, readsPerWorker = 16, 64
		value := []byte("payload")
		keys := make([]uuid.UUID, workers)
		for i := range keys {
			keys[i] = uuid.New()
			datastoreSet(keys[i], value)
		}
		userlib.DatastoreResetBandwidth()

		start := make(chan struct{})
		valid := make([]bool, workers)
		var wg sync.WaitGroup
		for i := range keys {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				valid[i] = true
				for n := 0; n < readsPerWorker; n++ {
					got, ok := datastoreGet(keys[i])
					if !ok || !bytes.Equal(got, value) {
						valid[i] = false
					}
				}
			}(i)
		}
		close(start)
		wg.Wait()

		for _, ok := range valid {
			Expect(ok).To(BeTrue())
		}
		// An exact semantic oracle, not a throughput or timing assertion.
		Expect(userlib.DatastoreGetBandwidth()).To(Equal(workers * readsPerWorker * len(value)))
	})
})
