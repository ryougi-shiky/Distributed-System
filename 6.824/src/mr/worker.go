package mr

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"net/rpc"
	"os"
	"sort"
	"time"

	"github.com/google/uuid"
)

const MaxRetryTimes int = 3
const OutputNameTemplate string = "mr-out-%d"

// for sorting by key.
type ByKey []KeyValue

// for sorting by key.
func (a ByKey) Len() int           { return len(a) }
func (a ByKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByKey) Less(i, j int) bool { return a[i].Key < a[j].Key }

// Map functions return a slice of KeyValue.
type KeyValue struct {
	Key   string
	Value string
}

// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}

func calculateReduceBucket(key string, nReduce int) int {
	return ihash(key) % nReduce
}

var Id uuid.UUID = uuid.New()
var ProcessedTasksNum int = 0
var TimeStartTask time.Time

// main/mrworker.go calls this function.
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {
	// log.Printf("[Worker] Worker %s is running!\n", Id)
	for {
		task := FetchTask()
		if task == nil {
			// log.Println("[Worker] Exiting...")
			return
		}
		ProcessedTasksNum++
		TimeStartTask = time.Now()
		switch task.Type {
		case WaitTask:
			ProcessedTasksNum--
			time.Sleep(WaitInterval * time.Second)
		case ExitTask:
			// log.Println("[Worker] Exiting...")
			return
		case MapTask:
			processMapTask(mapf, task)
			if !SubmitTask(task) {
				// log.Println("[Worker] Exiting...")
				return
			}
		case ReduceTask:
			processReduceTask(reducef, task)
			if !SubmitTask(task) {
				// log.Println("[Worker] Exiting...")
				return
			}
		}
	}
}

// Get task from coordinator. If failed, retry for MaxRetryTimes times.
func FetchTask() *Task {
	args := FetchTaskArgs{
		Msg:    fmt.Sprintf("processed %d tasks", ProcessedTasksNum),
		NodeId: Id,
	}
	var ok bool
	reply := FetchTaskReply{}
	retryTimes := 0
call:
	ok = call("Coordinator.FetchTask", &args, &reply)
	if ok {
		// log.Printf("[Worker] Ask Coordinator %s for task. Msg: %s\n", reply.NodeId, reply.Msg)
		return reply.Task
	} else {
		if retryTimes+1 == MaxRetryTimes {
			goto fail
		}
		retryTimes++
		// log.Printf("[Worker] Unable to call Coordinator, tried %d time(s)...\n", retryTimes)
		goto call
	}
fail:
	// log.Printf("[Worker] Failed to call Coordinator, tried %d times. Exiting gracefully.", MaxRetryTimes)
	return nil
}

func SubmitTask(task *Task) bool {
	args := SubmitTaskArgs{
		NodeId: Id,
		Msg:    fmt.Sprintf("took %d millseconds", time.Since(TimeStartTask).Microseconds()),
		Task:   task,
	}
	var ok bool
	reply := SubmitTaskReply{}
	retryTimes := 0
call:
	ok = call("Coordinator.SubmitTask", &args, &reply)
	if ok {
		// log.Printf("[Worker] Submit task to Coordinator %s. Msg: %s\n", reply.NodeId, reply.Msg)
		return true
	} else {
		if retryTimes+1 == MaxRetryTimes {
			goto fail
		}
		retryTimes++
		// log.Printf("[Worker] Unable to call Coordinator, tried %d time(s)...\n", retryTimes)
		goto call
	}
fail:
	// log.Printf("[Worker] Failed to call Coordinator, tried %d times. Exiting gracefully.", MaxRetryTimes)
	return false
}

func processMapTask(mapf func(string, string) []KeyValue, task *Task) {
	fileName := task.Files[0]
	// log.Printf("[Worker] Working on map task. File: %s, TaskId: %d\n", fileName, task.TaskId)
	file, err := os.Open(fileName)
	if err != nil {
		// log.Fatalf("[Worker] cannot open %s", fileName)
	}
	content, err := ioutil.ReadAll(file)
	if err != nil {
		// log.Fatalf("[Worker] cannot read %s", fileName)
	}
	file.Close()
	// Find all word count key-value pairs from the file.
	kva := mapf(fileName, string(content))
	intermediates := make([][]KeyValue, task.ReducerTaskNum)
	// Initialise reducer buckets
	for i := 0; i < task.ReducerTaskNum; i++ {
		intermediates[i] = []KeyValue{}
	}
	// Distribute key-value pairs to reducer buckets
	for _, kv := range kva {
		index := calculateReduceBucket(kv.Key, task.ReducerTaskNum)
		intermediates[index] = append(intermediates[index], kv)
	}
	for i := 0; i < task.ReducerTaskNum; i++ {
		tmpfile, err := ioutil.TempFile(".", "mrtmp-map")
		if err != nil {
			// log.Panic("[Worker] Failed to create map tmp file.", err)
		}
		enc := json.NewEncoder(tmpfile)
		for _, kv := range intermediates[i] {
			// write kv to tmpfile as json format
			err := enc.Encode(&kv)
			if err != nil {
				// log.Panic("[Worker] cannot encode KV.", err)
			}
		}
		intermediateFileName := fmt.Sprintf(IntermediateNameTemplate, task.TaskId, i)
		err = os.Rename(tmpfile.Name(), intermediateFileName)
		if err != nil {
			// log.Printf("[Worker] Rename tmpfile failed for %s\n", intermediateFileName)
		}
	}
}

func processReduceTask(reducef func(string, []string) string, task *Task) {
	// log.Printf("[Worker] Working on reduce task. TaskId: %d\n", task.TaskId)
	intermediate := []KeyValue{}
	for _, fileName := range task.Files {
		intermediate = append(intermediate, readIntermediateFile(fileName)...)
	}
	sort.Sort(ByKey(intermediate))
	ofile, err := ioutil.TempFile(".", "mrtmp-reduce")
	if err != nil {
		// log.Panic("[Worker] Failed to create reduce tmp file.", err)
	}
	// i is the index for the current group of key-value pairs being processed.
	i := 0
	for i < len(intermediate) {
		// j finds the next index where the key changes, marking the end of the current group.
		j := i + 1
		for j < len(intermediate) && intermediate[j].Key == intermediate[i].Key {
			j++
		}
		// Inside this loop, the values for a single key are accumulated into the values slice,
		// which is then passed to reducef to perform the reduce operation.
		values := []string{}
		for k := i; k < j; k++ {
			values = append(values, intermediate[k].Value)
		}
		output := reducef(intermediate[i].Key, values)

		// this is the correct format for each line of Reduce output.
		fmt.Fprintf(ofile, "%v %v\n", intermediate[i].Key, output)

		i = j
	}
	reduceFileName := fmt.Sprintf(OutputNameTemplate, task.TaskId)
	os.Rename(ofile.Name(), reduceFileName)
}

func readIntermediateFile(fileName string) []KeyValue {
	file, err := os.Open(fileName)
	defer file.Close()
	if err != nil {
		// log.Panicf("[Worker] cannot open file %v\n", fileName)
	}
	dec := json.NewDecoder(file)
	kva := []KeyValue{}
	kv := KeyValue{}
	for dec.Decode(&kv) == nil {
		kva = append(kva, kv)
	}
	return kva
}

// example function to show how to make an RPC call to the coordinator.
//
// the RPC argument and reply types are defined in rpc.go.
// func CallExample() {

// 	// declare an argument structure.
// 	args := ExampleArgs{}

// 	// fill in the argument(s).
// 	args.X = 99

// 	// declare a reply structure.
// 	reply := ExampleReply{}

// 	// send the RPC request, wait for the reply.
// 	// the "Coordinator.Example" tells the
// 	// receiving server that we'd like to call
// 	// the Example() method of struct Coordinator.
// 	ok := call("Coordinator.Example", &args, &reply)
// 	if ok {
// 		// reply.Y should be 100.
// 		fmt.Printf("reply.Y %v\n", reply.Y)
// 	} else {
// 		fmt.Printf("call failed!\n")
// 	}
// }

// send an RPC request to the coordinator, wait for the response.
// usually returns true.
// returns false if something goes wrong.
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := coordinatorSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		// log.Fatal("dialing:", err)
	}
	defer c.Close()

	err = c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}
