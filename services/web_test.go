package services

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// https://stackoverflow.com/questions/22892120/how-to-generate-a-random-string-of-a-fixed-length-in-go
const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

var src = rand.NewSource(time.Now().UnixNano())

func RandStringBytesMaskImprSrcSB(n int) string {
	sb := strings.Builder{}
	sb.Grow(n)
	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			sb.WriteByte(letterBytes[idx])
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return sb.String()
}

var tl = 1000000
var test = RandStringBytesMaskImprSrcSB(tl)
var c = &http.Client{}

type MyReader struct {
	*bytes.Reader
	callback func()
}

func (s MyReader) Read(b []byte) (n int, err error) {
	s.callback()
	return s.Reader.Read(b)
}

func getTestServer(num int, num2 int) *httptest.Server {
	b := []byte(test)
	br := bytes.NewReader(b)
	mbr := MyReader{Reader: br}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Etag", "\"aaa\"")
		http.ServeContent(w, r, "", time.Now(), mbr)
	}))
	i := num
	i2 := num2
	mbr.callback = func() {
		if i%10 == 0 {
			ts.CloseClientConnections()
		}
		i++
		log.Println(i)
		if num2 != 0 {
			if i2%10 == 0 {
				ts.CloseClientConnections()
			}
			i2++
		}
	}
	return ts
}

func TestProxyWithLateFail(t *testing.T) {
	ts := getTestServer(1, 0)
	defer ts.Close()
	u, _ := url.Parse(ts.URL)
	ps := httptest.NewServer(retryProxyHandler(c, u))
	defer ps.Close()
	r, err := c.Get(ps.URL + "/aaa")
	if err != nil {
		t.Fatalf("Expected err==nil got %v", err)
	}
	defer r.Body.Close()
	rb, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("Expected err==nil got %v", err)
	}
	if len(rb) != len(test) {
		t.Fatalf("Expected len(rb)==%v got %v", len(test), len(rb))
	}
	if string(rb) != test {
		t.Fatalf("Data mismatch")
	}
}

func TestProxyWithEarlyFail(t *testing.T) {
	ts := getTestServer(9, 0)
	defer ts.Close()
	u, _ := url.Parse(ts.URL)
	ps := httptest.NewServer(retryProxyHandler(c, u))
	defer ps.Close()
	r, err := c.Get(ps.URL + "/aaa")
	if err != nil {
		t.Fatalf("Expected err==nil got %v", err)
	}
	defer r.Body.Close()
	rb, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("Expected err==nil got %v", err)
	}
	if len(rb) != len(test) {
		t.Fatalf("Expected len(rb)==%v got %v", len(test), len(rb))
	}
	if string(rb) != test {
		t.Fatalf("Data mismatch")
	}
}

func TestProxyWithSeveralEarlyFails(t *testing.T) {
	ts := getTestServer(8, 9)
	defer ts.Close()
	u, _ := url.Parse(ts.URL)
	ps := httptest.NewServer(retryProxyHandler(c, u))
	defer ps.Close()
	r, err := c.Get(ps.URL + "/aaa")
	if err != nil {
		t.Fatalf("Expected err==nil got %v", err)
	}
	defer r.Body.Close()
	rb, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("Expected err==nil got %v", err)
	}
	if len(rb) != len(test) {
		t.Fatalf("Expected len(rb)==%v got %v", len(test), len(rb))
	}
	if string(rb) != test {
		t.Fatalf("Data mismatch")
	}
}

func TestProxyWithRangeRequestWithoutEnd(t *testing.T) {
	ts := getTestServer(1, 0)
	defer ts.Close()
	u, _ := url.Parse(ts.URL)
	ps := httptest.NewServer(retryProxyHandler(c, u))
	defer ps.Close()
	req, _ := http.NewRequest("GET", ps.URL+"/aaa", strings.NewReader(""))
	start := 1000
	req.Header.Set("Range", fmt.Sprintf("bytes=%v-%v", start, ""))
	r, err := c.Do(req)
	if err != nil {
		t.Fatalf("Expected err==nil got %v", err)
	}
	defer r.Body.Close()
	rb, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("Expected err==nil got %v", err)
	}
	if len(rb) != len(test)-start {
		t.Fatalf("Expected len(rb)==%v got %v", len(test)-start, len(rb))
	}
	if string(rb) != string([]byte(test)[start:]) {
		t.Fatalf("Data mismatch")
	}
}

func TestProxyWithRangeRequestWithEnd(t *testing.T) {
	ts := getTestServer(1, 0)
	defer ts.Close()
	u, _ := url.Parse(ts.URL)
	ps := httptest.NewServer(retryProxyHandler(c, u))
	defer ps.Close()
	req, _ := http.NewRequest("GET", ps.URL+"/aaa", strings.NewReader(""))
	start := 1000
	end := len(test) - start
	req.Header.Set("Range", fmt.Sprintf("bytes=%v-%v", start, end))
	r, err := c.Do(req)
	if err != nil {
		t.Fatalf("Expected err==nil got %v", err)
	}
	defer r.Body.Close()
	rb, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("Expected err==nil got %v", err)
	}
	if len(rb) != len(test)-2*start+1 {
		t.Fatalf("Expected len(rb)==%v got %v", len(test)-2*start+1, len(rb))
	}
	if string(rb) != string([]byte(test)[start:len(test)-start+1]) {
		t.Fatalf("Data mismatch")
	}
}
