package main

import (
	"container/list"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"time"
)

const MAX_BUFFER_SIZE = 50

var (
	bufferDadosSensoriais       *list.List // List that will serve as a queue
	bufferTarefas               *list.List
	wg                          *sync.WaitGroup // Synchronization variable
	mutexDadosSensoriais        *sync.Mutex
	mutexBufferTarefa           *sync.Mutex
	mutexTable                  *sync.Mutex
	mutexPrinting               *sync.Mutex
	cond_varDadosSensoriaisProd *sync.Cond // Condition variable to assist communication between goroutines and blocking (mutex). It's a rendezvous.
	cond_varDadosSensoriaisCons *sync.Cond
	cond_varBufferTarefaProd    *sync.Cond
	cond_varBufferTarefaCons    *sync.Cond
	N_SENSORES                  int
	N_ATUADORES                 int
	N_UNIDADES_PROCESSAMENTO    int
	table                       *map[int]MutexStruct

	// Colors for output
	Reset  = "\033[0m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Orange = "\033[33m"
)

type Tarefa struct {
	atuador   int
	atividade int
}

type MutexStruct struct {
	Value int
	Mutex *sync.Mutex
}

func newTarefa(atuador int, atividade int) Tarefa {
	return Tarefa{
		atuador:   atuador,
		atividade: atividade,
	}
}

func sensorProdutor() {
	for {
		data := rand.Intn(1001)
		time.Sleep(time.Duration(rand.Intn(4000)+1000) * time.Millisecond) // Random time between 1 and 5 seconds
		//time.Sleep(1 * time.Second)

		cond_varDadosSensoriaisProd.L.Lock()

		fmt.Printf("Size of the sensory data buffer: %d\n", bufferDadosSensoriais.Len())

		for bufferDadosSensoriais.Len() >= MAX_BUFFER_SIZE {
			cond_varDadosSensoriaisProd.Wait()
			fmt.Println(Green + "[PRODUCER 1 START WAITING...]" + Reset)
		}

		bufferDadosSensoriais.PushBack(data) // Adds the sensory data to the end of the queue

		cond_varDadosSensoriaisCons.Signal() // Resumes a goroutine that was waiting for data: cond_varDadosSensoriaisCons.Wait()

		cond_varDadosSensoriaisProd.L.Unlock()
	}
}

func consumidorCentralControle() {
	for {
		cond_varDadosSensoriaisCons.L.Lock()

		for bufferDadosSensoriais.Len() == 0 {
			cond_varDadosSensoriaisCons.Wait() // If the sensory data queue is empty, wait until a producer adds new sensory data.
			fmt.Println(Orange + "[CONSUMER 1 START WAITING...]" + Reset)
		}

		ds := bufferDadosSensoriais.Front() // Getting the first value in the list
		bufferDadosSensoriais.Remove(ds)    // Removing this value

		cond_varDadosSensoriaisProd.Signal()

		cond_varDadosSensoriaisCons.L.Unlock()

		atuador := ds.Value.(int) % N_ATUADORES // Getting the actuator that will have its activity level changed
		atividade := rand.Intn(101)             // Getting the activity to be changed in the actuator
		tarefa := newTarefa(atuador, atividade) // Instantiating a Tarefa struct

		// time.Sleep(500 * time.Millisecond)

		cond_varBufferTarefaProd.L.Lock()

		fmt.Printf("Size of the task buffer: %d\n", bufferTarefas.Len())

		for bufferTarefas.Len() >= MAX_BUFFER_SIZE {
			cond_varBufferTarefaProd.Wait()
			fmt.Println(Green + "[PRODUCER 2 START WAITING...]" + Reset)
		}

		bufferTarefas.PushBack(tarefa) // Adding a task to the task list

		cond_varBufferTarefaCons.Signal() // Resumes a goroutine that was waiting for data: cond_varBufferTarefaCons.Wait()

		cond_varBufferTarefaProd.L.Unlock()
	}
}

func realizaTarefa() {
	for {
		cond_varBufferTarefaCons.L.Lock()

		for bufferTarefas.Len() == 0 {
			cond_varBufferTarefaCons.Wait() // If the task queue is empty, wait until the control center adds a new task.
			fmt.Println(Orange + "[CONSUMER 2 START WAITING...]" + Reset)
		}

		tarefa := bufferTarefas.Front() // Getting the first value in the list
		bufferTarefas.Remove(tarefa)    // Removing this value

		cond_varBufferTarefaProd.Signal()

		cond_varBufferTarefaCons.L.Unlock()

		tarefaValue := tarefa.Value.(Tarefa)

		var wgFork sync.WaitGroup // Declaring a synchronization variable

		ok := make(chan bool, 2) // Channel with buffer size 2 to get failure or success results of subtasks. Buffered channels act like queues
		wgFork.Add(2)            // Adding two goroutines (subtasks) to be awaited (counter = 2)

		// Fork:
		go mudancaAtuador(ok, tarefaValue.atuador, tarefaValue.atividade, &wgFork) // Subtask to change the activity of an actuator
		go painel(ok, tarefaValue.atuador, tarefaValue.atividade, &wgFork)         // Subtask to print on the screen that it is changing the activity of the actuator

		//Join:
		wgFork.Wait() // Waiting for the two goroutines to finish, that is, when the counter = 0

		close(ok) // Closing the channel to avoid conflict with other goroutines
		for result := range ok {
			if result == false { // If any channel assigned a false value to the channel, the failure is printed
				mutexPrinting.Lock() // Controlling logging to the terminal
				fmt.Printf(Red+"Failure: %d with value %d\n"+Reset, tarefaValue.atuador, tarefaValue.atividade)
				mutexPrinting.Unlock()
				break // Break because, for example, if both values of the channel are false, we should only print the failure once
			}
		}
	}
}

func mudancaAtuador(ok chan bool, atuador, atividade int, wg *sync.WaitGroup) {
	defer wg.Done()          // Defer decrementing the WaitGroup counter until the function ends
	if rand.Intn(100) < 20 { // 20% chance of failure
		ok <- false // Sending failure message through the channel
	} else {
		mutexTable.Lock()         // Lock access to the map to retrieve the activity and mutex related to the actuator
		v, _ := (*table)[atuador] // Get the MutexStruct linked to the actuator
		mutexTable.Unlock()       // Unlock access to the map as soon as the value is obtained

		v.Mutex.Lock()                                                     // Lock access to the specific actuator in the table
		v.Value = atividade                                                // Modify its activity level
		time.Sleep(time.Duration(rand.Intn(1000)+2000) * time.Millisecond) // Random time between 2 and 3 seconds
		v.Mutex.Unlock()                                                   // Unlock access to the specific actuator in the table

		mutexTable.Lock()     // Lock access to the map to update the actuator value
		(*table)[atuador] = v // Update actuator activity in the map
		mutexTable.Unlock()   // Unlock access to the map

		ok <- true // Sending success message through the channel
	}
}

func painel(ok chan bool, atuador, atividade int, wg *sync.WaitGroup) {
	defer wg.Done()          // Defer decrementing the WaitGroup counter until the function ends
	if rand.Intn(100) < 20 { // 20% chance of failure
		ok <- false // Sending failure message through the channel
	} else {
		mutexPrinting.Lock()
		fmt.Printf("Changing: %d with value %d\n", atuador, atividade)
		time.Sleep(1 * time.Second)
		mutexPrinting.Unlock()
		ok <- true // Sending success message through the channel
	}
}

func main() {
	fmt.Print("Choose the number of sensors: ")
	fmt.Scanln(&N_SENSORES)
	fmt.Print("Choose the number of actuators: ")
	fmt.Scanln(&N_ATUADORES)

	N_UNIDADES_PROCESSAMENTO = runtime.NumCPU()

	bufferDadosSensoriais = list.New() // New list (will be used as a queue)
	mutexDadosSensoriais = new(sync.Mutex)
	cond_varDadosSensoriaisProd = sync.NewCond(mutexDadosSensoriais) // Two condition variables with the same mutex to access the same buffer
	cond_varDadosSensoriaisCons = sync.NewCond(mutexDadosSensoriais)

	table = &map[int]MutexStruct{}
	mutexTable = new(sync.Mutex)

	bufferTarefas = list.New()
	mutexBufferTarefa = new(sync.Mutex)
	cond_varBufferTarefaProd = sync.NewCond(mutexBufferTarefa) // Two condition variables with the same mutex to access the same buffer
	cond_varBufferTarefaCons = sync.NewCond(mutexBufferTarefa)

	mutexPrinting = new(sync.Mutex)

	for i := 0; i < N_ATUADORES; i++ {
		(*table)[i] = MutexStruct{
			Value: 0,             // Assigning zero to the activity levels of each actuator in the table
			Mutex: &sync.Mutex{}, // Each actuator will have its activity level and its mutex
		}
	}

	wg = &sync.WaitGroup{}
	wg.Add(1) // Adding one goroutine to be awaited, but since the goroutines declared below are in an infinite loop, they will never end

	// Producers:
	for i := 0; i < N_SENSORES; i++ {
		go sensorProdutor() // Multiple producers (number established by the N_SENSORES variable)
	}

	// Consumer:
	go consumidorCentralControle()

	// Thread pool:
	for i := 0; i < N_UNIDADES_PROCESSAMENTO; i++ {
		go realizaTarefa() // Multiple processing units ready to perform tasks
	}

	wg.Wait() // Waiting for some goroutine to end, but since the goroutines declared above are in infinite loops, they will never finish
	// The program remains stuck here while the goroutines are doing their respective work
}
