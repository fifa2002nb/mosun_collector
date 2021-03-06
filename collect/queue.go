package collect

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"time"

	log "github.com/Sirupsen/logrus"
	"mosun_collector/opentsdb"
)

func queuer() {
	for dp := range tchan {
		qlock.Lock()
		for {
			if len(queue) > MaxQueueLen {
				slock.Lock()
				dropped++
				slock.Unlock()
				break
			}
			queue = append(queue, dp)
			select {
			case dp = <-tchan:
				continue
			default:
			}
			break
		}
		qlock.Unlock()
	}
}

func send() {
	for {
		qlock.Lock()
		if i := len(queue); i > 0 {
			if i > BatchSize {
				i = BatchSize
			}
			sending := queue[:i]
			queue = queue[i:]
			log.Debugf("sending: %d, remaining: %d", i, len(queue))
			qlock.Unlock()
			Sample("collect.post.batchsize", Tags, float64(len(sending)))
			sendBatch(sending)
		} else {
			qlock.Unlock()
			time.Sleep(time.Second)
		}
	}
}

func sendBatch(batch []*opentsdb.DataPoint) {
	if Print {
		for _, d := range batch {
			j, err := d.MarshalJSON()
			if err != nil {
				log.Error(err)
			}
			log.Info(string(j))
		}
		recordSent(len(batch))
		return
	}
	now := time.Now()
	resp, err := SendDataPoints(batch, tsdbURL)
	if err == nil {
		defer resp.Body.Close()
	}
	d := time.Since(now).Nanoseconds() / 1e6
	Sample("collect.post.duration", Tags, float64(d))
	// Some problem with connecting to the server; retry later.
	if err != nil || resp.StatusCode != http.StatusNoContent {
		if err != nil {
			log.Error(err)
		} else if resp.StatusCode != http.StatusNoContent {
			log.Errorln(resp.Status)
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				log.Error(err)
			}
			if len(body) > 0 {
				log.Error(string(body))
			}
		}
		restored := 0
		for _, msg := range batch { //发送失败后，重新发
			restored++
			tchan <- msg
		}
		d := time.Second * 5
		log.Infof("restored %d, sleeping %s", restored, d)
		time.Sleep(d)
		return
	}
	recordSent(len(batch))
}

func recordSent(num int) {
	log.Debug("sent", num)
	slock.Lock()
	sent += int64(num)
	slock.Unlock()
}

func SendDataPoints(dps []*opentsdb.DataPoint, tsdb string) (*http.Response, error) {
	var buf bytes.Buffer
	g := gzip.NewWriter(&buf)
	if err := json.NewEncoder(g).Encode(dps); err != nil {
		return nil, err
	}
	if err := g.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", tsdb, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	resp, err := client.Do(req)
	return resp, err
}
