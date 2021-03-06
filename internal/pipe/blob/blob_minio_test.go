package blob

// this is pretty much copied from the s3 pipe to ensure both work the same way
// only differences are that it sets `blobs` instead of `s3` on test cases and
// the test setup and teardown

import (
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/goreleaser/goreleaser/internal/artifact"
	"github.com/goreleaser/goreleaser/pkg/config"
	"github.com/goreleaser/goreleaser/pkg/context"
	"github.com/stretchr/testify/require"
	"gocloud.dev/blob"
)

func TestMinioUpload(t *testing.T) {
	var listen = randomListen(t)
	folder, err := ioutil.TempDir("", "goreleasertest")
	require.NoError(t, err)
	srcpath := filepath.Join(folder, "source.tar.gz")
	tgzpath := filepath.Join(folder, "bin.tar.gz")
	debpath := filepath.Join(folder, "bin.deb")
	checkpath := filepath.Join(folder, "check.txt")
	require.NoError(t, ioutil.WriteFile(checkpath, []byte("fake checksums"), 0744))
	require.NoError(t, ioutil.WriteFile(srcpath, []byte("fake\nsrc"), 0744))
	require.NoError(t, ioutil.WriteFile(tgzpath, []byte("fake\ntargz"), 0744))
	require.NoError(t, ioutil.WriteFile(debpath, []byte("fake\ndeb"), 0744))
	var ctx = context.New(config.Project{
		Dist:        folder,
		ProjectName: "testupload",
		Blobs: []config.Blob{
			{
				Provider: "s3",
				Bucket:   "test",
				Region:   "us-east",
				Endpoint: "http://" + listen,
				IDs:      []string{"foo", "bar"},
			},
		},
	})
	ctx.Git = context.GitInfo{CurrentTag: "v1.0.0"}
	ctx.Artifacts.Add(&artifact.Artifact{
		Type: artifact.Checksum,
		Name: "checksum.txt",
		Path: checkpath,
	})
	ctx.Artifacts.Add(&artifact.Artifact{
		Type: artifact.UploadableSourceArchive,
		Name: "source.tar.gz",
		Path: srcpath,
		Extra: map[string]interface{}{
			"Format": "tar.gz",
		},
	})
	ctx.Artifacts.Add(&artifact.Artifact{
		Type: artifact.UploadableArchive,
		Name: "bin.tar.gz",
		Path: tgzpath,
		Extra: map[string]interface{}{
			"ID": "foo",
		},
	})
	ctx.Artifacts.Add(&artifact.Artifact{
		Type: artifact.LinuxPackage,
		Name: "bin.deb",
		Path: debpath,
		Extra: map[string]interface{}{
			"ID": "bar",
		},
	})
	var name = "test_upload"
	defer stop(t, name)
	start(t, name, listen)
	prepareEnv(t, listen)
	require.NoError(t, Pipe{}.Default(ctx))
	require.NoError(t, Pipe{}.Publish(ctx))

	require.Subset(t, getFiles(t, ctx, ctx.Config.Blobs[0]), []string{
		"testupload/v1.0.0/bin.deb",
		"testupload/v1.0.0/bin.tar.gz",
		"testupload/v1.0.0/checksum.txt",
		"testupload/v1.0.0/source.tar.gz",
	})
}

func TestMinioUploadCustomBucketID(t *testing.T) {
	var listen = randomListen(t)
	folder, err := ioutil.TempDir("", "goreleasertest")
	require.NoError(t, err)
	tgzpath := filepath.Join(folder, "bin.tar.gz")
	debpath := filepath.Join(folder, "bin.deb")
	require.NoError(t, ioutil.WriteFile(tgzpath, []byte("fake\ntargz"), 0744))
	require.NoError(t, ioutil.WriteFile(debpath, []byte("fake\ndeb"), 0744))
	// Set custom BUCKET_ID env variable.
	err = os.Setenv("BUCKET_ID", "test")
	require.NoError(t, err)
	var ctx = context.New(config.Project{
		Dist:        folder,
		ProjectName: "testupload",
		Blobs: []config.Blob{
			{
				Provider: "s3",
				Bucket:   "{{.Env.BUCKET_ID}}",
				Endpoint: "http://" + listen,
			},
		},
	})
	ctx.Git = context.GitInfo{CurrentTag: "v1.0.0"}
	ctx.Artifacts.Add(&artifact.Artifact{
		Type: artifact.UploadableArchive,
		Name: "bin.tar.gz",
		Path: tgzpath,
	})
	ctx.Artifacts.Add(&artifact.Artifact{
		Type: artifact.LinuxPackage,
		Name: "bin.deb",
		Path: debpath,
	})
	var name = "custom_bucket_id"
	defer stop(t, name)
	start(t, name, listen)
	prepareEnv(t, listen)
	require.NoError(t, Pipe{}.Default(ctx))
	require.NoError(t, Pipe{}.Publish(ctx))
}

func TestMinioUploadInvalidCustomBucketID(t *testing.T) {
	var listen = randomListen(t)
	folder, err := ioutil.TempDir("", "goreleasertest")
	require.NoError(t, err)
	tgzpath := filepath.Join(folder, "bin.tar.gz")
	debpath := filepath.Join(folder, "bin.deb")
	require.NoError(t, ioutil.WriteFile(tgzpath, []byte("fake\ntargz"), 0744))
	require.NoError(t, ioutil.WriteFile(debpath, []byte("fake\ndeb"), 0744))
	// Set custom BUCKET_ID env variable.
	require.NoError(t, err)
	var ctx = context.New(config.Project{
		Dist:        folder,
		ProjectName: "testupload",
		Blobs: []config.Blob{
			{
				Provider: "s3",
				Bucket:   "{{.Bad}}",
				Endpoint: "http://" + listen,
			},
		},
	})
	ctx.Git = context.GitInfo{CurrentTag: "v1.1.0"}
	ctx.Artifacts.Add(&artifact.Artifact{
		Type: artifact.UploadableArchive,
		Name: "bin.tar.gz",
		Path: tgzpath,
	})
	ctx.Artifacts.Add(&artifact.Artifact{
		Type: artifact.LinuxPackage,
		Name: "bin.deb",
		Path: debpath,
	})
	var name = "invalid_bucket_id"
	defer stop(t, name)
	start(t, name, listen)
	prepareEnv(t, listen)
	require.NoError(t, Pipe{}.Default(ctx))
	require.Error(t, Pipe{}.Publish(ctx))
}

func TestMinioUploadSkipPublish(t *testing.T) {
	var listen = randomListen(t)
	folder, err := ioutil.TempDir("", "goreleasertest")
	require.NoError(t, err)
	srcpath := filepath.Join(folder, "source.tar.gz")
	tgzpath := filepath.Join(folder, "bin.tar.gz")
	debpath := filepath.Join(folder, "bin.deb")
	checkpath := filepath.Join(folder, "check.txt")
	require.NoError(t, ioutil.WriteFile(checkpath, []byte("fake checksums"), 0744))
	require.NoError(t, ioutil.WriteFile(srcpath, []byte("fake\nsrc"), 0744))
	require.NoError(t, ioutil.WriteFile(tgzpath, []byte("fake\ntargz"), 0744))
	require.NoError(t, ioutil.WriteFile(debpath, []byte("fake\ndeb"), 0744))
	var ctx = context.New(config.Project{
		Dist:        folder,
		ProjectName: "testupload",
		Blobs: []config.Blob{
			{
				Provider: "s3",
				Bucket:   "test",
				Region:   "us-east",
				Endpoint: "http://" + listen,
				IDs:      []string{"foo", "bar"},
			},
		},
	})
	ctx.SkipPublish = true
	ctx.Git = context.GitInfo{CurrentTag: "v1.2.0"}
	ctx.Artifacts.Add(&artifact.Artifact{
		Type: artifact.Checksum,
		Name: "checksum.txt",
		Path: checkpath,
	})
	ctx.Artifacts.Add(&artifact.Artifact{
		Type: artifact.UploadableSourceArchive,
		Name: "source.tar.gz",
		Path: srcpath,
		Extra: map[string]interface{}{
			"Format": "tar.gz",
		},
	})
	ctx.Artifacts.Add(&artifact.Artifact{
		Type: artifact.UploadableArchive,
		Name: "bin.tar.gz",
		Path: tgzpath,
		Extra: map[string]interface{}{
			"ID": "foo",
		},
	})
	ctx.Artifacts.Add(&artifact.Artifact{
		Type: artifact.LinuxPackage,
		Name: "bin.deb",
		Path: debpath,
		Extra: map[string]interface{}{
			"ID": "bar",
		},
	})
	var name = "test_upload"
	defer stop(t, name)
	start(t, name, listen)
	prepareEnv(t, listen)
	require.NoError(t, Pipe{}.Default(ctx))
	require.NoError(t, Pipe{}.Publish(ctx))

	require.NotContains(t, getFiles(t, ctx, ctx.Config.Blobs[0]), []string{
		"testupload/v1.2.0/bin.deb",
		"testupload/v1.2.0/bin.tar.gz",
		"testupload/v1.2.0/checksum.txt",
		"testupload/v1.2.0/source.tar.gz",
	})
}

func randomListen(t *testing.T) string {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	listener.Close()
	return listener.Addr().String()
}

func prepareEnv(t *testing.T, listen string) {
	os.Setenv("AWS_ACCESS_KEY_ID", "minio")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "miniostorage")
	os.Setenv("AWS_REGION", "us-east-1")
}

func start(t *testing.T, name, listen string) {
	wd, err := os.Getwd()
	require.NoError(t, err)

	removeTestData(t)

	if out, err := exec.Command(
		"docker", "run", "-d", "--rm",
		"-v", filepath.Join(wd, "testdata/data")+":/data",
		"--name", name,
		"-p", listen+":9000",
		"-e", "MINIO_ACCESS_KEY=minio",
		"-e", "MINIO_SECRET_KEY=miniostorage",
		"--health-interval", "1s",
		"--health-cmd=curl --silent --fail http://localhost:9000/minio/health/ready || exit 1",
		"minio/minio",
		"server", "/data",
	).CombinedOutput(); err != nil {
		t.Fatalf("failed to start minio: %s", string(out))
	}

	for range time.Tick(time.Second) {
		out, err := exec.Command("docker", "inspect", "--format='{{json .State.Health}}'", name).CombinedOutput()
		if err != nil {
			t.Fatalf("failed to check minio status: %s", string(out))
		}
		if strings.Contains(string(out), `"Status":"healthy"`) {
			t.Log("minio is healthy")
			break
		}
		t.Log("waiting for minio to be healthy")
	}
}

func stop(t *testing.T, name string) {
	if out, err := exec.Command("docker", "stop", name).CombinedOutput(); err != nil {
		t.Fatalf("failed to stop minio: %s", string(out))
	}
	removeTestData(t)
}

func removeTestData(t *testing.T) {
	_ = os.RemoveAll("./testdata/data/test/testupload") // dont care if it fails
}

func getFiles(t *testing.T, ctx *context.Context, cfg config.Blob) []string {
	url, err := urlFor(ctx, cfg)
	require.NoError(t, err)
	conn, err := blob.OpenBucket(ctx, url)
	require.NoError(t, err)
	defer conn.Close()
	var iter = conn.List(nil)
	var files []string
	for {
		file, err := iter.Next(ctx)
		if err != nil && err == io.EOF {
			break
		}
		require.NoError(t, err)
		files = append(files, file.Key)
	}
	return files
}
