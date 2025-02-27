/*
 * JuiceFS, Copyright 2018 Juicedata, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package object

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	_ "github.com/mattn/go-sqlite3"
)

func get(s ObjectStorage, k string, off, limit int64) (string, error) {
	r, err := s.Get(k, off, limit)
	if err != nil {
		return "", err
	}
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func listAll(s ObjectStorage, prefix, marker string, limit int64) ([]Object, error) {
	r, err := s.List(prefix, marker, limit)
	if !errors.Is(err, notSupported) {
		return r, nil
	}
	ch, err := s.ListAll(prefix, marker)
	if err == nil {
		objs := make([]Object, 0)
		for obj := range ch {
			if len(objs) < int(limit) {
				objs = append(objs, obj)
			}
		}
		return objs, nil
	}
	return nil, err
}

// nolint:errcheck
func testStorage(t *testing.T, s ObjectStorage) {
	if err := s.Create(); err != nil {
		t.Fatalf("Can't create bucket %s: %s", s, err)
	}
	if err := s.Create(); err != nil {
		t.Fatalf("err should be nil when creating a bucket with the same name")
	}
	s = WithPrefix(s, "unit-test/")
	defer s.Delete("test")
	k := "large"
	defer s.Delete(k)

	_, err := s.Get("not_exists", 0, -1)
	if err == nil {
		t.Fatalf("Get should failed: %s", err)
	}

	br := []byte("hello")
	if err := s.Put("test", bytes.NewReader(br)); err != nil {
		t.Fatalf("PUT failed: %s", err.Error())
	}

	if d, e := get(s, "test", 0, -1); d != "hello" {
		t.Fatalf("expect hello, but got %v, error: %s", d, e)
	}
	if d, e := get(s, "test", 2, -1); d != "llo" {
		t.Fatalf("expect llo, but got %v, error: %s", d, e)
	}
	if d, e := get(s, "test", 2, 3); d != "llo" {
		t.Fatalf("expect llo, but got %v, error: %s", d, e)
	}
	if d, e := get(s, "test", 2, 2); d != "ll" {
		t.Fatalf("expect ll, but got %v, error: %s", d, e)
	}
	if d, e := get(s, "test", 4, 2); d != "o" {
		t.Errorf("out-of-range get: 'o', but got %v, error: %s", len(d), e)
	}
	if d, e := get(s, "test", 6, 2); d != "" {
		t.Errorf("out-of-range get: '', but got %v, error: %s", len(d), e)
	}
	switch s.(*withPrefix).os.(type) {
	case FileSystem:
		objs, err2 := listAll(s, "", "", 2)
		if err2 == nil {
			if len(objs) != 2 {
				t.Fatalf("List should return 2 keys, but got %d", len(objs))
			}
			if objs[0].Key() != "" {
				t.Fatalf("First key should be empty string, but got %s", objs[0].Key())
			}
			if objs[0].Size() != 0 {
				t.Fatalf("First object size should be 0, but got %d", objs[0].Size())
			}
			if objs[1].Key() != "test" {
				t.Fatalf("First key should be test, but got %s", objs[1].Key())
			}
			if !strings.Contains(s.String(), "encrypted") && objs[1].Size() != 5 {
				t.Fatalf("Size of first key shold be 5, but got %v", objs[1].Size())
			}
			now := time.Now()
			if objs[1].Mtime().Before(now.Add(-30*time.Second)) || objs[1].Mtime().After(now.Add(time.Second*30)) {
				t.Fatalf("Mtime of key should be within 10 seconds, but got %s", objs[1].Mtime().Sub(now))
			}
		} else {
			t.Fatalf("list failed: %s", err2.Error())
		}

		objs, err2 = listAll(s, "", "test2", 1)
		if err2 != nil {
			t.Fatalf("list3 failed: %s", err2.Error())
		} else if len(objs) != 0 {
			t.Fatalf("list3 should not return anything, but got %d", len(objs))
		}
	default:
		objs, err2 := listAll(s, "", "", 1)
		if err2 == nil {
			if len(objs) != 1 {
				t.Fatalf("List should return 1 keys, but got %d", len(objs))
			}
			if objs[0].Key() != "test" {
				t.Fatalf("First key should be test, but got %s", objs[0].Key())
			}
			if !strings.Contains(s.String(), "encrypted") && objs[0].Size() != 5 {
				t.Fatalf("Size of first key shold be 5, but got %v", objs[0].Size())
			}
			now := time.Now()
			if objs[0].Mtime().Before(now.Add(-30*time.Second)) || objs[0].Mtime().After(now.Add(time.Second*30)) {
				t.Fatalf("Mtime of key should be within 10 seconds, but got %s", objs[0].Mtime().Sub(now))
			}
		} else {
			t.Fatalf("list failed: %s", err2.Error())
		}

		objs, err2 = listAll(s, "", "test2", 1)
		if err2 != nil {
			t.Fatalf("list3 failed: %s", err2.Error())
		} else if len(objs) != 0 {
			t.Fatalf("list3 should not return anything, but got %d", len(objs))
		}
	}

	f, _ := ioutil.TempFile("", "test")
	f.Write([]byte("this is a file"))
	f.Seek(0, 0)
	os.Remove(f.Name())
	defer f.Close()
	if err := s.Put("file", f); err != nil {
		t.Fatalf("failed to put from file")
	} else if _, err := s.Head("file"); err != nil {
		t.Fatalf("file should exists")
	} else {
		s.Delete("file")
	}

	if _, err := s.Head("test"); err != nil {
		t.Fatalf("check exists failed: %s", err.Error())
	}

	if err := s.Delete("test"); err != nil {
		t.Fatalf("delete failed: %s", err)
	}

	if err := s.Delete("test"); err != nil {
		t.Fatalf("delete non exists: %v", err)
	}

	if uploader, err := s.CreateMultipartUpload(k); err == nil {
		partSize := uploader.MinPartSize
		uploadID := uploader.UploadID
		defer s.AbortUpload(k, uploadID)

		part1, err := s.UploadPart(k, uploadID, 1, make([]byte, partSize))
		if err != nil {
			t.Fatalf("UploadPart 1 failed: %s", err)
		}
		if pending, marker, err := s.ListUploads(""); err != nil {
			t.Logf("ListMultipart fail: %s", err.Error())
		} else {
			println(len(pending), marker)
		}
		part2Size := 1 << 20
		_, err = s.UploadPart(k, uploadID, 2, make([]byte, part2Size))
		if err != nil {
			t.Fatalf("UploadPart 2 failed: %s", err)
		}
		part2Size = 2 << 20
		part2, err := s.UploadPart(k, uploadID, 2, make([]byte, part2Size))
		if err != nil {
			t.Fatalf("UploadPart 2 failed: %s", err)
		}

		if err := s.CompleteUpload(k, uploadID, []*Part{part1, part2}); err != nil {
			t.Fatalf("CompleteMultipart failed: %s", err.Error())
		}
		if in, err := s.Get(k, 0, -1); err != nil {
			t.Fatalf("large not exists")
		} else if d, err := ioutil.ReadAll(in); err != nil {
			t.Fatalf("fail to read large file")
		} else if len(d) != partSize+part2Size {
			t.Fatalf("size of large file: %d != %d", len(d), partSize+part2Size)
		}
	} else {
		t.Logf("%s does not support multipart upload: %s", s, err.Error())
	}

	// Copy empty objects
	defer s.Delete("empty")
	if err := s.Put("empty", bytes.NewReader([]byte{})); err != nil {
		t.Logf("PUT empty object failed: %s", err.Error())
	}

	// Copy `/` suffixed object
	defer s.Delete("slash/")
	if err := s.Put("slash/", bytes.NewReader([]byte{})); err != nil {
		t.Logf("PUT `/` suffixed object failed: %s", err.Error())
	}
}

func TestMem(t *testing.T) {
	m, _ := newMem("", "", "")
	testStorage(t, m)
}

func TestDisk(t *testing.T) {
	s, _ := newDisk("/tmp/abc/", "", "")
	testStorage(t, s)
}

func TestQingStor(t *testing.T) {
	if os.Getenv("QY_ACCESS_KEY") == "" {
		t.SkipNow()
	}
	s, _ := newQingStor("https://test.pek3a.qingstor.com",
		os.Getenv("QY_ACCESS_KEY"), os.Getenv("QY_SECRET_KEY"))
	testStorage(t, s)

	//private cloud
	if os.Getenv("PRIVATE_QY_ACCESS_KEY") == "" {
		t.SkipNow()
	}
	s2, _ := newQingStor("http://test.jn1.is.shanhe.com",
		os.Getenv("PRIVATE_QY_ACCESS_KEY"), os.Getenv("PRIVATE_QY_SECRET_KEY"))
	testStorage(t, s2)
}

func TestS3(t *testing.T) {
	if os.Getenv("AWS_ACCESS_KEY_ID") == "" {
		t.SkipNow()
	}
	s, _ := newS3(fmt.Sprintf("https://%s", os.Getenv("S3_TEST_BUCKET")),
		os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY"))
	testStorage(t, s)
}

func TestOSS(t *testing.T) {
	if os.Getenv("ALICLOUD_ACCESS_KEY_ID") == "" {
		t.SkipNow()
	}
	bucketName := "test"
	if b := os.Getenv("OSS_TEST_BUCKET"); b != "" {
		bucketName = b
	}
	s, _ := newOSS(fmt.Sprintf("https://%s", bucketName),
		os.Getenv("ALICLOUD_ACCESS_KEY_ID"), os.Getenv("ALICLOUD_ACCESS_KEY_SECRET"))
	testStorage(t, s)
}

func TestUFile(t *testing.T) {
	if os.Getenv("UCLOUD_PUBLIC_KEY") == "" {
		t.SkipNow()
	}
	ufile, _ := newUFile("https://test.us-ca.ufileos.com",
		os.Getenv("UCLOUD_PUBLIC_KEY"), os.Getenv("UCLOUD_PRIVATE_KEY"))
	testStorage(t, ufile)
}

func TestGS(t *testing.T) {
	if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") == "" {
		t.SkipNow()
	}
	gs, _ := newGS("gs://zhijian-test/", "", "")
	testStorage(t, gs)
}

func TestQiniu(t *testing.T) {
	if os.Getenv("QINIU_ACCESS_KEY") == "" {
		t.SkipNow()
	}
	qiniu, _ := newQiniu("https://test.cn-east-1-s3.qiniu.com",
		os.Getenv("QINIU_ACCESS_KEY"), os.Getenv("QINIU_SECRET_KEY"))
	testStorage(t, qiniu)
	qiniu, _ = newQiniu("https://test.cn-north-1-s3.qiniu.com",
		os.Getenv("QINIU_ACCESS_KEY"), os.Getenv("QINIU_SECRET_KEY"))
	testStorage(t, qiniu)
}

func TestKS3(t *testing.T) {
	if os.Getenv("KS3_ACCESS_KEY") == "" {
		t.SkipNow()
	}
	ks3, _ := newKS3("https://test.kss.ksyun.com",
		os.Getenv("KS3_ACCESS_KEY"), os.Getenv("KS3_SECRET_KEY"))
	testStorage(t, ks3)
}

func TestCOS(t *testing.T) {
	if os.Getenv("COS_SECRETID") == "" {
		t.SkipNow()
	}
	cos, _ := newCOS(
		fmt.Sprintf("https://%s", os.Getenv("COS_TEST_BUCKET")),
		os.Getenv("COS_SECRETID"), os.Getenv("COS_SECRETKEY"))
	testStorage(t, cos)
}

func TestAzure(t *testing.T) {
	if os.Getenv("AZURE_STORAGE_ACCOUNT") == "" {
		t.SkipNow()
	}
	abs, _ := newWabs("https://test-chunk.core.chinacloudapi.cn",
		os.Getenv("AZURE_STORAGE_ACCOUNT"), os.Getenv("AZURE_STORAGE_KEY"))
	testStorage(t, abs)
}

func TestNOS(t *testing.T) {
	if os.Getenv("NOS_ACCESS_KEY") == "" {
		t.SkipNow()
	}
	nos, _ := newNOS("https://test.nos-eastchina1.126.net",
		os.Getenv("NOS_ACCESS_KEY"), os.Getenv("NOS_SECRET_KEY"))
	testStorage(t, nos)
}

func TestMSS(t *testing.T) {
	if os.Getenv("MSS_ACCESS_KEY") == "" {
		t.SkipNow()
	}
	mss, _ := newMSS("https://test.mtmss.com",
		os.Getenv("MSS_ACCESS_KEY"), os.Getenv("MSS_SECRET_KEY"))
	testStorage(t, mss)
}

func TestJSS(t *testing.T) {
	if os.Getenv("JSS_ACCESS_KEY") == "" {
		t.SkipNow()
	}
	jss, _ := newJSS("https://test.s3.cn-north-1.jcloudcs.com",
		os.Getenv("JSS_ACCESS_KEY"), os.Getenv("JSS_SECRET_KEY"))
	testStorage(t, jss)
}

func TestSpeedy(t *testing.T) {
	if os.Getenv("SPEEDY_ACCESS_KEY") == "" {
		t.SkipNow()
	}
	cos, _ := newSpeedy("https://test.oss-cn-beijing.speedycloud.org",
		os.Getenv("SPEEDY_ACCESS_KEY"), os.Getenv("SPEEDY_SECRET_KEY"))
	testStorage(t, cos)
}

func TestB2(t *testing.T) {
	if os.Getenv("B2_ACCOUNT_ID") == "" {
		t.SkipNow()
	}
	b, err := newB2("https://jfs-test.backblaze.com", os.Getenv("B2_ACCOUNT_ID"), os.Getenv("B2_APP_KEY"))
	if err != nil {
		t.Fatalf("create B2: %s", err)
	}
	testStorage(t, b)
}

func TestSpace(t *testing.T) {
	if os.Getenv("SPACE_ACCESS_KEY") == "" {
		t.SkipNow()
	}
	b, _ := newSpace("https://test.nyc3.digitaloceanspaces.com", os.Getenv("SPACE_ACCESS_KEY"), os.Getenv("SPACE_SECRET_KEY"))
	testStorage(t, b)
}

func TestBOS(t *testing.T) {
	if os.Getenv("BDCLOUD_ACCESS_KEY") == "" {
		t.SkipNow()
	}
	b, _ := newBOS(fmt.Sprintf("https://%s", os.Getenv("BOS_TEST_BUCKET")),
		os.Getenv("BDCLOUD_ACCESS_KEY"), os.Getenv("BDCLOUD_SECRET_KEY"))
	testStorage(t, b)
}

func TestSftp(t *testing.T) {
	if os.Getenv("SFTP_HOST") == "" {
		t.SkipNow()
	}
	b, _ := newSftp(os.Getenv("SFTP_HOST"), os.Getenv("SFTP_USER"), os.Getenv("SFTP_PASS"))
	testStorage(t, b)
}

func TestOBS(t *testing.T) {
	if os.Getenv("HWCLOUD_ACCESS_KEY") == "" {
		t.SkipNow()
	}
	b, _ := newOBS(fmt.Sprintf("https://%s", os.Getenv("OBS_TEST_BUCKET")),
		os.Getenv("HWCLOUD_ACCESS_KEY"), os.Getenv("HWCLOUD_SECRET_KEY"))
	testStorage(t, b)
}

func TestHDFS(t *testing.T) {
	if os.Getenv("HDFS_ADDR") == "" {
		t.Skip()
	}
	dfs, _ := newHDFS(os.Getenv("HDFS_ADDR"), "", "")
	testStorage(t, dfs)
}

func TestOOS(t *testing.T) {
	if os.Getenv("OOS_ACCESS_KEY") == "" {
		t.SkipNow()
	}
	b, _ := newOOS(fmt.Sprintf("https://%s", os.Getenv("OOS_TEST_BUCKET")),
		os.Getenv("OOS_ACCESS_KEY"), os.Getenv("OOS_SECRET_KEY"))
	testStorage(t, b)
}

func TestScw(t *testing.T) {
	if os.Getenv("SCW_ACCESS_KEY") == "" {
		t.SkipNow()
	}
	b, _ := newScw(fmt.Sprintf("https://%s", os.Getenv("SCW_TEST_BUCKET")), os.Getenv("SCW_ACCESS_KEY"), os.Getenv("SCW_SECRET_KEY"))
	testStorage(t, b)
}

func TestMinIO(t *testing.T) {
	if os.Getenv("MINIO_TEST_BUCKET") == "" {
		t.SkipNow()
	}
	b, _ := newMinio(fmt.Sprintf("http://%s/some/path", os.Getenv("MINIO_TEST_BUCKET")), "", "")
	testStorage(t, b)
}

// func TestUpYun(t *testing.T) {
// 	s, _ := newUpyun("http://jfstest", "test", "")
// 	testStorage(t, s)
// }

func TestYovole(t *testing.T) {
	if os.Getenv("OS2_TEST_BUCKET") == "" {
		t.SkipNow()
	}
	s, _ := newYovole(os.Getenv("OS2_TEST_BUCKET"), os.Getenv("OS2_ACCESS_KEY"), os.Getenv("OS2_SECRET_KEY"))
	testStorage(t, s)
}

func TestTiKV(t *testing.T) {
	if os.Getenv("TIKV_ADDR") == "" {
		t.SkipNow()
	}
	s, err := newTiKV(os.Getenv("TIKV_ADDR"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	testStorage(t, s)
}
func TestRedis(t *testing.T) {
	if os.Getenv("REDIS_ADDR") == "" {
		t.SkipNow()
	}

	opt, _ := redis.ParseURL(os.Getenv("REDIS_ADDR"))
	rdb := redis.NewClient(opt)
	_ = rdb.FlushDB(context.Background())

	s, err := newRedis(os.Getenv("REDIS_ADDR"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	testStorage(t, s)
}

func TestSwift(t *testing.T) {
	if os.Getenv("SWIFT_ADDR") == "" {
		t.SkipNow()
	}
	s, err := newSwiftOSS(os.Getenv("SWIFT_ADDR"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	testStorage(t, s)
}

func TestWebDAV(t *testing.T) {
	if os.Getenv("WEBDAV_TEST_BUCKET") == "" {
		t.SkipNow()
	}
	s, _ := newWebDAV(os.Getenv("WEBDAV_TEST_BUCKET"), "", "")
	testStorage(t, s)
}

func TestEncrypted(t *testing.T) {
	s, _ := CreateStorage("mem", "", "", "")
	privkey, _ := rsa.GenerateKey(rand.Reader, 2048)
	kc := NewRSAEncryptor(privkey)
	dc := NewAESEncryptor(kc)
	es := NewEncrypted(s, dc)
	testStorage(t, es)
}

func TestMarsharl(t *testing.T) {
	if os.Getenv("HDFS_ADDR") == "" {
		t.Skip()
	}
	s, _ := newHDFS(os.Getenv("HDFS_ADDR"), "", "")
	if err := s.Put("hello", bytes.NewReader([]byte("world"))); err != nil {
		t.Fatalf("PUT failed: %s", err)
	}
	fs := s.(FileSystem)
	_ = fs.Chown("hello", "user", "group")
	_ = fs.Chmod("hello", 0764)
	o, err := s.Head("hello")
	if err != nil {
		t.Fatalf("HEAD failed: %s", err)
	}

	m := MarshalObject(o)
	d, _ := json.Marshal(m)
	var m2 map[string]interface{}
	if err := json.Unmarshal(d, &m2); err != nil {
		t.Fatalf("unmarshal: %s", err)
	}
	o2 := UnmarshalObject(m2)
	if math.Abs(float64(o2.Mtime().UnixNano()-o.Mtime().UnixNano())) > 1000 {
		t.Fatalf("mtime %s != %s", o2.Mtime(), o.Mtime())
	}
	o2.(*file).mtime = o.Mtime()
	if !reflect.DeepEqual(o, o2) {
		t.Fatalf("%+v != %+v", o2, o)
	}
}

func TestSharding(t *testing.T) {
	s, _ := NewSharded("mem", "%d", "", "", 10)
	testStorage(t, s)
}

func TestSQLite(t *testing.T) {
	s, err := newSQLStore("sqlite3", "/tmp/teststore.db", "", "")
	if err != nil {
		t.Fatalf("create: %s", err)
	}
	testStorage(t, s)
}

func TestPG(t *testing.T) {
	s, err := newSQLStore("postgres", "localhost:5432/test?sslmode=disable", "", "")
	if err == nil {
		testStorage(t, s)
	}
}

func TestMySQL(t *testing.T) {
	s, err := newSQLStore("mysql", "/dev", "root", "")
	if err == nil {
		testStorage(t, s)
	}
}

func TestNameString(t *testing.T) {
	s, _ := newMem("test", "", "")
	s = WithPrefix(s, "a/")
	s = WithPrefix(s, "b/")
	if s.String() != "mem://test/a/b/" {
		t.Fatalf("name with two prefix does not match: %s", s.String())
	}
}

func TestEtcd(t *testing.T) {
	s, _ := newEtcd("127.0.0.1:2379", "", "")
	testStorage(t, s)
}
