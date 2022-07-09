package transfer_pvc

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type RsyncLogStreamReader struct {
	r io.ReadCloser
}

func (r RsyncLogStreamReader) Read(b []byte) (n int, err error) {
	buf := make([]byte, 32*1024)
	n, err = r.r.Read(buf)
	updatedLog := generateProgressLog(string(buf[:n]))
	copy(b, []byte(updatedLog))
	return len(b), err
}

func (r RsyncLogStreamReader) Close() error {
	return r.r.Close()
}

func generateProgressLog(originalLog string) string {
	deleter := "\x1b[1A\x1b[2K" // "\033[F"
	progressLog := ""
	intVal := func(i *int64) string {
		if i == nil {
			return "Unavailable"
		}
		return fmt.Sprintf("%d", i)
	}
	dataVal := func(i *dataSize) string {
		if i == nil {
			return "Unavailable"
		}
		return i.String()
	}
	p, parseError := parseRsyncLogs(originalLog)
	if parseError != nil {
		progressLog = "\rProgress unavailable\n"
	} else {

		progressLog = fmt.Sprintf(
			"Status: %s\nTotal failes: %s\nData Transferred: %s\nRate: %s\n",
			p.Status(), intVal(p.totalFiles),
			dataVal(p.dataTransferred), dataVal(p.transferRate))
	}
	for i := 0; i < strings.Count(progressLog, "\n"); i++ {
		progressLog = fmt.Sprintf("%s%s", deleter, progressLog)
	}
	return progressLog
}

// progress defines transfer progress
type progress struct {
	transferPercentage *int64
	totalFiles         *int64
	totalDirs          *int64
	transferRate       *dataSize
	dataTransferred    *dataSize
	files              []string
	failedFiles        []string
	exitCode           *int
}

type dataSize struct {
	size float64
	unit string
}

func (d dataSize) String() string {
	return fmt.Sprintf("%f %s", d.size, d.unit)
}

type status string

const (
	succeeded       status = "succeeded"
	failed          status = "failed"
	partiallyFailed status = "partiallyFailed"
	inProgress      status = "inProgress"
)

// Status returns a type of completion for transfer
func (p progress) Status() status {
	if p.exitCode != nil {
		if *p.exitCode == 0 {
			return succeeded
		}
		if len(p.files) == 0 && p.dataTransferred == nil && p.totalFiles != nil {
			return failed
		}
		return partiallyFailed
	}
	return inProgress
}

func newDataSize(str string) *dataSize {
	r := regexp.MustCompile(`([\d.]+)([\w\/]+)`)
	matched := r.FindStringSubmatch(str)
	if len(matched) < 2 {
		return nil
	}
	size, err := strconv.ParseFloat(matched[1], 64)
	if err != nil {
		return nil
	}
	unit := matched[2]
	return &dataSize{
		size: size,
		unit: unit,
	}
}

func parseRsyncLogs(stdout string) (progress, error) {
	p := progress{
		files:       []string{},
		failedFiles: []string{},
	}
	fileProgressRegex := regexp.MustCompile(`([\d.]+\w+)[\t ]+(\d+)%[\t ]+([\d.]+\w{1,2}\/\w+).*<f\++ (.*)`)
	errorRegex := regexp.MustCompile(`.*send_files.*"(.*)": (.*)`)
	fileCountRegex := regexp.MustCompile(`Number of files: (\d+).*reg: (\d+),.*(\d+).*`)

	for _, line := range strings.Split(stdout, "\n") {
		// in-progress information
		for _, matched := range fileProgressRegex.FindAllStringSubmatch(line, -1) {
			// transferred data
			if len(matched) > 1 {
				p.dataTransferred = newDataSize(matched[1])
			}
			// percentage
			if len(matched) > 2 {
				percentage, err := strconv.ParseInt(matched[2], 10, 64)
				if err == nil {
					p.transferPercentage = &percentage
				}
			}
			// speed
			if len(matched) > 3 {
				p.transferRate = newDataSize(matched[3])
			}
			// file name
			if len(matched) > 4 {
				p.files = append(p.files, matched[4])
			}
		}
		// post-completion transfer stats
		for _, matched := range fileCountRegex.FindAllStringSubmatch(line, -1) {
			if len(matched) > 2 {
				if val, err := strconv.ParseInt(matched[2], 10, 64); err == nil {
					p.totalFiles = &val
				}
			}
			if len(matched) > 3 {
				if val, err := strconv.ParseInt(matched[3], 10, 64); err == nil {
					p.totalDirs = &val
				}
			}
		}
	}

	for _, line := range strings.Split(stdout, "\n") {
		for _, matched := range errorRegex.FindAllStringSubmatch(line, -1) {
			if len(matched) > 1 {
				p.failedFiles = append(p.failedFiles, matched[1])
			}
		}
	}

	return p, nil
}

func followClientLogs(srcConfig *rest.Config, c client.Client, namespace string, labels map[string]string) error {
	clientPod := &corev1.Pod{}

	err := wait.PollUntil(time.Second, func() (done bool, err error) {
		clientPodList := &corev1.PodList{}

		err = c.List(context.Background(), clientPodList, client.InNamespace(namespace), client.MatchingLabels(labels))
		if err != nil {
			return false, err
		}

		if len(clientPodList.Items) != 1 {
			log.Printf("expected 1 client pod found %d, with labels %v\n", len(clientPodList.Items), labels)
			return false, nil
		}

		clientPod = &clientPodList.Items[0]

		for _, containerStatus := range clientPod.Status.ContainerStatuses {
			if containerStatus.State.Terminated != nil && containerStatus.State.Terminated.ExitCode == 0 {
				log.Printf("container %s in pod %s completed successfully", containerStatus.Name, client.ObjectKey{Namespace: namespace, Name: clientPod.Name})
				break
			}
			if !containerStatus.Ready {
				log.Println(fmt.Errorf("container %s in pod %s is not ready", containerStatus.Name, client.ObjectKey{Namespace: namespace, Name: clientPod.Name}))
				return false, nil
			}
		}
		return true, nil
	}, make(<-chan struct{}))
	if err != nil {
		return err
	}

	clienset, err := kubernetes.NewForConfig(srcConfig)
	if err != nil {
		return err
	}

	podLogsRequest := clienset.CoreV1().Pods(namespace).GetLogs(clientPod.Name, &corev1.PodLogOptions{
		TypeMeta:  metav1.TypeMeta{},
		Container: "rsync",
		Follow:    true,
	})

	reader, err := podLogsRequest.Stream(context.Background())
	if err != nil {
		return err
	}
	rsyncLogStreamReader := RsyncLogStreamReader{
		r: reader,
	}
	defer rsyncLogStreamReader.Close()
	_, err = io.Copy(os.Stdout, rsyncLogStreamReader)
	if err != nil {
		return err
	}

	return err
}
