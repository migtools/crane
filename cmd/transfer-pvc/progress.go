package transfer_pvc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type rsyncLogStream struct {
	pvc        types.NamespacedName
	podLabels  map[string]string
	restCfg    *rest.Config
	stdout     chan string
	stderr     chan string
	err        chan error
	progress   *Progress
	outputFile *string
}

func NewRsyncLogStream(restCfg *rest.Config, pvc types.NamespacedName, labels map[string]string, output string) LogStreams {
	var outputFile string
	if output != "" {
		outputFile = output
	}
	return &rsyncLogStream{
		restCfg:    restCfg,
		pvc:        pvc,
		podLabels:  labels,
		outputFile: &outputFile,
	}
}

func (r *rsyncLogStream) Init() error {
	r.stdout = make(chan string)
	r.stderr = make(chan string)
	r.err = make(chan error)

	clientset, err := kubernetes.NewForConfig(r.restCfg)
	if err != nil {
		return err
	}

	podName, err := waitForPodRunning(clientset, r.pvc.Namespace, r.podLabels)
	if err != nil {
		return err
	}

	podLogsRequest := clientset.CoreV1().Pods(r.pvc.Namespace).GetLogs(podName, &corev1.PodLogOptions{
		TypeMeta:  metav1.TypeMeta{},
		Container: "rsync",
		Follow:    true,
	})

	podLogStream, err := podLogsRequest.Stream(context.TODO())
	if err != nil {
		return err
	}

	r.progress = NewProgress(r.pvc)
	var lastProgress *Progress

	go func() {
		defer podLogStream.Close()
		logString := ""
		zeroBytes := 0
		for {
			buf := make([]byte, 32*1024)
			n, readErr := podLogStream.Read(buf)
			if n > 0 {
				zeroBytes = 0
			} else {
				zeroBytes += 1
			}
			// sometimes, a stream would end without returning an EOF gracefully
			// we force exit the loop when we see null bytes on stream consecutively
			if zeroBytes > 4 {
				err = io.EOF
			}
			logString = fmt.Sprintf("%s%s", logString, string(buf[:n]))
			if readErr == io.EOF {
				err = readErr
				// attempt to get a final status of terminated pod
				code, finalLogs, e := getFinalPodStatus(clientset, podName, r.pvc.Namespace)
				if e != nil {
					err = e
				}
				r.progress.ExitCode = code
				logString = finalLogs
			}
			parsedProgress, unparsed := parseRsyncLogs(logString)
			r.progress.Merge(parsedProgress)
			outString, errString := r.progress.AsString()
			if len(outString) > 0 {
				// overwrite previous progress
				if lastProgress != nil {
					oldStdOut, _ := lastProgress.AsString()
					for i := 0; i < strings.Count(oldStdOut, "\n"); i++ {
						outString = fmt.Sprintf("\x1b[1A\x1b[2K%s", outString)
					}
				}
				r.stdout <- outString
			}
			if len(errString) > 0 {
				r.stderr <- errString
			}
			logString = unparsed
			lastProgress = r.progress
			if err != nil {
				r.err <- err
				break
			}
		}
		if r.outputFile != nil {
			writeProgressToFile(*r.outputFile, r.progress)
		}
	}()

	return nil
}

func writeProgressToFile(o string, p *Progress) error {
	file, err := os.OpenFile(o, os.O_CREATE, os.ModePerm)
	if err != nil {
		return err
	}
	defer file.Close()
	d, _ := json.MarshalIndent(p, "", "  ")
	return ioutil.WriteFile(o, d, os.ModePerm)
}

func (r *rsyncLogStream) Close() {
	close(r.stdout)
	close(r.stderr)
	close(r.err)
}

func (r *rsyncLogStream) Streams() (stdout chan string, stderr chan string, err chan error) {
	return r.stdout, r.stderr, r.err
}

// Progress defines transfer Progress
type Progress struct {
	PVC                types.NamespacedName `json:"pvc"`
	TransferPercentage *int64               `json:"transferPercentage"`
	TransferRate       *dataSize            `json:"transferRate"`
	TransferredData    *dataSize            `json:"transferredData"`
	TotalFiles         *int64               `json:"totalFiles"`
	TransferredFiles   int64                `json:"transferredFiles"`
	ExitCode           *int32               `json:"exitCode"`
	FailedFiles        []FailedFile         `json:"failedFiles"`
	Errors             []string             `json:"miscErrors"`
	retries            *int
	startedAt          time.Time
}

// pastAttempts stores cumulative progress info
// of all previous attempts of transfer
var pastAttempts Progress

// failedFiles cache of discovered files
var failedFiles map[string]bool

type FailedFile struct {
	Name string `json:"name"`
	Err  string `json:"error"`
}

type dataSize struct {
	val  float64
	unit string
}

func addDataSize(a, b *dataSize) *dataSize {
	if b == nil {
		return nil
	}
	newDs := &dataSize{}
	units := map[string]int{"bytes": 0, "K": 3, "M": 6, "G": 9, "T": 12}
	if b.unit == a.unit {
		newDs.val = b.val + a.val
		newDs.unit = b.unit
	} else {
		if nu, exists := units[b.unit]; exists {
			if du, exists := units[a.unit]; exists {
				if nu > du {
					newDs.val = b.val + (a.val / math.Pow(10, float64(nu-du)))
					newDs.unit = b.unit
				} else {
					newDs.val = (b.val / math.Pow(10, float64(du-nu))) + a.val
					newDs.unit = a.unit
				}
			}
		}
	}
	return newDs
}

func (d *dataSize) String() string {
	return fmt.Sprintf("%.2f %s", d.val, d.unit)
}

func (d *dataSize) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (p *Progress) AsString() (out string, err string) {
	progressLog := ""
	intVal := func(i *int64, pref string) string {
		if i == nil {
			return "<unavailable>"
		}
		return fmt.Sprintf("%d%s", *i, pref)
	}
	dataVal := func(i *dataSize) string {
		if i == nil {
			return "<unavailable>"
		}
		return i.String()
	}
	progressLog = fmt.Sprintf("%sStatus:\t%s\n", progressLog, p.Status())
	progressLog = fmt.Sprintf("%sProgress:\n", progressLog)
	progressLog = fmt.Sprintf("%s  Percentage:\t%s\n", progressLog, intVal(p.TransferPercentage, "%"))
	progressLog = fmt.Sprintf("%s  Transferred:\t%s\n", progressLog, dataVal(p.TransferredData))
	progressLog = fmt.Sprintf("%s  Rate:\t\t%s\n", progressLog, dataVal(p.TransferRate))
	if p.retries != nil {
		progressLog = fmt.Sprintf("%s  Retries:\t%d\n", progressLog, *p.retries)
	}
	if p.Status().Completed() {
		progressLog = fmt.Sprintf("%s  Files:\n", progressLog)
		progressLog = fmt.Sprintf("%s    Sent:\t%d\n", progressLog, p.TransferredFiles)
		if p.TotalFiles != nil {
			progressLog = fmt.Sprintf("%s    Total:\t%d\n", progressLog, *p.TotalFiles)
		}
	}
	progressLog = fmt.Sprintf("%sElapsed:\t%s\n", progressLog, time.Since(p.startedAt).Round(time.Second).String())
	errors, failedFiles := "", ""
	if p.Status().Completed() {
		if len(p.FailedFiles) > 0 {
			failedFiles = "Failed files: \n"
			for _, f := range p.FailedFiles {
				failedFiles = fmt.Sprintf("%s  - %s [%s]\n", failedFiles, f.Name, f.Err)
			}
		}
		if len(p.Errors) > 0 {
			errors := "Errors: \n"
			for _, e := range p.Errors {
				errors = fmt.Sprintf("%s - %s\n", errors, e)
			}
		}
	}
	return progressLog, fmt.Sprintf("%s%s", errors, failedFiles)
}

func NewProgress(name types.NamespacedName) *Progress {
	return &Progress{
		PVC:         name,
		FailedFiles: make([]FailedFile, 0),
		Errors:      make([]string, 0),
		startedAt:   time.Now(),
	}
}

type status string

const (
	succeeded          status = "Succeeded"
	failed             status = "Failed"
	partiallyFailed    status = "Partially failed"
	preparing          status = "Preparing"
	transferInProgress status = "Transfer in-progress"
	finishingUp        status = "Finishing up"
)

func (s status) Completed() bool {
	return s == succeeded || s == failed || s == partiallyFailed
}

// Status returns current status of transfer
func (p *Progress) Status() status {
	if p.ExitCode != nil {
		if *p.ExitCode == 0 {
			int100 := int64(100)
			p.TransferPercentage = &int100
			return succeeded
		}
		if p.TransferredFiles == 0 &&
			p.TransferredData.val == 0 &&
			p.TotalFiles == nil {
			return failed
		}
		return partiallyFailed
	} else {
		if p.TransferPercentage == nil {
			return preparing
		}
		if *p.TransferPercentage >= 100 {
			return finishingUp
		}
	}
	return transferInProgress
}

// Merge merges two progress objects
func (p *Progress) Merge(in *Progress) {
	p.TransferredFiles += in.TransferredFiles
	if in.TotalFiles != nil {
		p.TotalFiles = in.TotalFiles
	}
	if in.ExitCode != nil {
		p.ExitCode = in.ExitCode
	}
	if in.TransferRate != nil {
		p.TransferRate = in.TransferRate
	}
	// aggregate percentage of all retries
	var totalPercentage *int64
	if pastAttempts.TransferPercentage != nil {
		totalPercentage = pastAttempts.TransferPercentage
	}
	if in.TransferPercentage != nil {
		if totalPercentage == nil {
			totalPercentage = in.TransferPercentage
		} else {
			t := *totalPercentage + *in.TransferPercentage
			totalPercentage = &t
		}
	}
	if totalPercentage != nil {
		if (p.TransferPercentage == nil) || (*totalPercentage <= int64(100) && *totalPercentage > *p.TransferPercentage) {
			p.TransferPercentage = totalPercentage
		}
	}
	// aggregate transferred data of all retries
	var totalTransferredData *dataSize
	if pastAttempts.TransferredData != nil {
		totalTransferredData = pastAttempts.TransferredData
	}
	if in.TransferredData != nil {
		if totalTransferredData == nil {
			totalTransferredData = in.TransferredData
		} else {
			t := addDataSize(totalTransferredData, in.TransferredData)
			totalTransferredData = t
		}
	}
	if totalTransferredData != nil {
		if (p.TransferredData == nil) || (totalTransferredData.val > p.TransferredData.val) {
			p.TransferredData = totalTransferredData
		}
	}
	if in.retries != nil {
		pastAttempts = *p
		p.retries = in.retries
	}
	p.Errors = append(p.Errors, in.Errors...)
	p.FailedFiles = append(p.FailedFiles, in.FailedFiles...)
}

func newDataSize(str string) *dataSize {
	r := regexp.MustCompile(`([\d\.]+)([\w\/]*)`)
	matched := r.FindStringSubmatch(str)
	if len(matched) < 2 {
		return nil
	}
	size, err := strconv.ParseFloat(matched[1], 64)
	if err != nil {
		return nil
	}
	unit := matched[2]
	if unit == "" {
		unit = "bytes"
	}
	return &dataSize{
		val:  size,
		unit: unit,
	}
}

// parseRsyncLogs parses raw rsync logs and returns a structured progress
// also returns data that was not processed, this is useful because log
// stream can have incomplete lines
func parseRsyncLogs(rawLogs string) (p *Progress, unprocessedData string) {
	p = NewProgress(types.NamespacedName{})
	// in-progress information
	fileProgressRegex := regexp.MustCompile(`([\d.]+\w+)[\t ]+(\d+)%[\t ]+([\d\.]+\w{1,2}\/\w+).*\(xfr.*\)`)
	fileErrorRegex := regexp.MustCompile(`rsync: \w+ "(.*)".*: (.*)`)
	processErrorRegex := regexp.MustCompile(`@ERROR: (.*)`)
	// final stats
	fileStatsRegex := regexp.MustCompile(`Number of files: (\d+).*reg: ([\d,]+), dir: ([\d,]+)`)
	finalDataTransferredRegex := regexp.MustCompile(`Total transferred file size: (.*) bytes`)
	finalFileCountRegex := regexp.MustCompile(`Number of regular files transferred: (.*)`)
	unprocessedLines := regexp.MustCompile(`.*?\n(.*)$`)
	// retries
	retryRegex := regexp.MustCompile(`Syncronization failed. Retrying in \d+ seconds. Retry (\d+)/.*`)

	inProgressLines := fileProgressRegex.FindAllStringSubmatch(rawLogs, -1)
	for _, matched := range inProgressLines {
		// transferred data
		if len(matched) > 1 {
			p.TransferredData = newDataSize(matched[1])
		}
		// percentage
		if len(matched) > 2 {
			observedPercentage, err := strconv.ParseInt(matched[2], 10, 64)
			if err == nil {
				p.TransferPercentage = &observedPercentage
			}
		}
		// speed
		if len(matched) > 3 {
			p.TransferRate = newDataSize(matched[3])
		}
	}
	// post-completion transfer stats
	for _, matched := range fileStatsRegex.FindAllStringSubmatch(rawLogs, -1) {
		if len(matched) > 2 {
			matched[2] = strings.ReplaceAll(matched[2], ",", "")
			if val, err := strconv.ParseInt(matched[2], 10, 64); err == nil {
				p.TotalFiles = &val
			}
		}
	}
	for _, matched := range fileErrorRegex.FindAllStringSubmatch(rawLogs, -1) {
		if len(matched) > 2 {
			if failedFiles == nil {
				failedFiles = make(map[string]bool)
			}
			if _, exists := failedFiles[matched[1]]; !exists {
				p.FailedFiles = append(p.FailedFiles, FailedFile{
					Name: matched[1],
					Err:  matched[2],
				})
				failedFiles[matched[1]] = true
			}
		}
	}
	for _, matched := range processErrorRegex.FindAllStringSubmatch(rawLogs, -1) {
		if len(matched) > 1 {
			p.Errors = append(p.Errors, matched[1])
		}
	}
	if matched := finalDataTransferredRegex.FindStringSubmatch(rawLogs); len(matched) > 1 {
		p.TransferredData = newDataSize(matched[1])
	}
	if matched := retryRegex.FindStringSubmatch(rawLogs); len(matched) > 1 {
		if val, err := strconv.Atoi(matched[1]); err == nil {
			p.retries = &val
		}
	}
	if matched := finalFileCountRegex.FindStringSubmatch(rawLogs); len(matched) > 1 {
		matched[1] = strings.ReplaceAll(matched[1], ",", "")
		if val, err := strconv.ParseInt(matched[1], 10, 64); err == nil {
			p.TransferredFiles = val
		}
	}
	if matched := unprocessedLines.FindStringSubmatch(rawLogs); len(matched) > 1 {
		return p, matched[1]
	}
	return p, ""
}

func waitForPodRunning(c *kubernetes.Clientset, namespace string, labels map[string]string) (string, error) {
	var podName string
	err := wait.PollUntil(time.Second, func() (done bool, err error) {
		listOptions := &client.ListOptions{}
		client.InNamespace(namespace).ApplyToList(listOptions)
		client.MatchingLabels(labels).ApplyToList(listOptions)
		clientPodList, err := c.CoreV1().Pods(namespace).List(context.TODO(), *listOptions.AsListOptions())
		if err != nil {
			return false, err
		}

		if len(clientPodList.Items) != 1 {
			log.Printf("expected 1 client pod found %d, with labels %v\n", len(clientPodList.Items), labels)
			return false, nil
		}

		clientPod := &clientPodList.Items[0]
		podName = clientPod.Name

		for _, containerStatus := range clientPod.Status.ContainerStatuses {
			if containerStatus.State.Terminated != nil {
				log.Printf("container %s in pod %s completed", containerStatus.Name, client.ObjectKey{Namespace: namespace, Name: clientPod.Name})
				break
			}
			if !containerStatus.Ready {
				log.Println(fmt.Errorf("container %s in pod %s is not ready", containerStatus.Name, client.ObjectKey{Namespace: namespace, Name: clientPod.Name}))
				return false, nil
			}
		}
		return true, nil
	}, make(<-chan struct{}))
	return podName, err
}

func getFinalPodStatus(c *kubernetes.Clientset, name string, namespace string) (*int32, string, error) {
	var exitCode *int32
	count := 0
	for {
		count += 1
		pod, err := c.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return nil, "", err
		}

		for _, container := range pod.Status.ContainerStatuses {
			if container.Name == "rsync" {
				if container.State.Terminated != nil {
					exitCode = &container.State.Terminated.ExitCode
				}
			}
		}
		if count > 5 || exitCode != nil {
			break
		}
	}

	lastLines := int64(35)
	finalLogRequest := c.CoreV1().Pods(namespace).GetLogs(name, &corev1.PodLogOptions{
		TypeMeta:  metav1.TypeMeta{},
		Container: "rsync",
		TailLines: &lastLines,
	})

	podLogStream, err := finalLogRequest.Stream(context.TODO())
	if err != nil {
		return exitCode, "", err
	}
	defer podLogStream.Close()

	buf := new(strings.Builder)
	_, err = io.Copy(buf, podLogStream)
	if err != nil {
		return exitCode, buf.String(), err
	}

	return exitCode, buf.String(), nil
}
