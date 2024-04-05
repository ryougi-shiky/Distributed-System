package mr

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"io/ioutil"
	"log"
	"net/rpc"
	"os"
	"sort"
)

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

// main/mrworker.go calls this function.
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {

	// Your worker implementation here.

	// uncomment to send the Example RPC to the coordinator.
	// CallExample()

	for {
		// declare an argument structure.
		args := TaskRequest{}
		// declare a reply structure.
		reply := TaskResponse{}
		// uncomment to send the Example RPC to the coordinator.
		CallGetTask(&args, &reply)
		filename := reply.RespTask.FileName
		id := reply.RespTask.ID
		state := reply.State

		if state == 1 && len(filename) != 0 {
			file, err := os.Open(filename)
			if err != nil {
				log.Fatalf("cannot open map task: %v\n", filename)
			}
			content, err := io.ReadAll(file)
			if err != nil {
				log.Fatalf("cannot read map task: %v\n", filename)
			}
			file.Close()

			kva := mapf(filename, string(content))
			bucket := make([][]KeyValue, reply.NumReduceTasks)
			for _, kv := range kva {
				// Split the kva into buckets
				bucket[ihash(kv.Key)%reply.NumReduceTasks] = append(bucket[ihash(kv.Key)%reply.NumReduceTasks], kv)
			}
			for i := 0; i < reply.NumReduceTasks; i++ {
				intermediate := fmt.Sprintf("mr-%d-%d", id, i)
				file, err := os.Create(intermediate)
				if err != nil {
					log.Fatalf("cannot create intermediate file: %v\n", intermediate)
				}
				for _, kv := range kva {
					if ihash(kv.Key)%reply.NumReduceTasks == i {
						fmt.Fprintf(file, "%v %v\n", kv.Key, kv.Value)
					}
				}
				file.Close()
			}
			CallFinishTask(&TaskRequest{X: id}, &TaskResponse{})
		} else if state == 2 {
			if reply.NumMapTasks == len(reply.MapTasksFin) {
				intermediate := []KeyValue{}
				for i := 0; i < reply.NumMapTasks; i++ {
					mapFileName := fmt.Sprintf("mr-%d-%d", id, i)
					inputFile, err := os.OpenFile(mapFileName, os.O_RDONLY, 0777)
					if err != nil {
						log.Fatalf("cannot open reduce task: %v\n", mapFileName)
					}
					decode := json.NewDecoder(inputFile)
					for {
						var kv []KeyValue
						if err := decode.Decode(&kv); err != nil {
							break
						}
						intermediate = append(intermediate, kv...)
					}
				}
				sort.Sort(ByKey(intermediate))
				outFileName := fmt.Sprintf("mr-out-%d", id)
				tmpFile, err := ioutil.TempFile("", "mr-tmp-*")
				if err != nil {
					log.Fatalf("cannot create temp file: %v\n", outFileName)
				}
				i := 0
				for i < len(intermediate) {
					j := i + 1
					for j < len(intermediate) && intermediate[j].Key == intermediate[i].Key {
						j++
					}
					values := []string{}
					for k := i; k < j; k++ {
						values = append(values, intermediate[k].Value)
					}
					output := reducef(intermediate[i].Key, values)
					fmt.Fprintf(tmpFile, "%v %v\n", intermediate[i].Key, output)
					i = j
				}
				tmpFile.Close()
				os.Rename(tmpFile.Name(), outFileName)
			}
			CallFinishTask(&TaskRequest{X: id}, &TaskResponse{})
		} else {
			break
		}

	}
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

func CallGetTask(args *TaskRequest, reply *TaskResponse) {

	// send the RPC request, wait for the reply.
	// the "Coordinator.GetTask" tells the
	// receiving server that we'd like to call
	// the GetTask() method of struct Coordinator.
	ok := call("Coordinator.GetTask", &args, &reply)
	if ok {
		// reply.Y should be 100.
		fmt.Printf("call get task successfully!\n")
	} else {
		fmt.Printf("call get task failed!\n")
	}
}

func CallFinishTask(args *TaskRequest, reply *TaskResponse) {
	ok := call("Coordinator.FinishTask", &args, &reply)
	if ok {
		// reply.Y should be 100.
		fmt.Printf("call finish task successfully!\n")
	} else {
		fmt.Printf("call finish task failed!\n")
	}
}

// send an RPC request to the coordinator, wait for the response.
// usually returns true.
// returns false if something goes wrong.
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := coordinatorSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		log.Fatal("dialing:", err)
	}
	defer c.Close()

	err = c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}
