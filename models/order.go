package models

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"hackcaptureorder/eventhub"

	"html/template"
	"log"
	"math/big"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Microsoft/ApplicationInsights-Go/appinsights"
	"github.com/streadway/amqp"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

// The order map
var (
	OrderList map[string]*Order
)

var (
	database string
	password string
	status   string
)

var username string
var address []string
var isAzure bool
var session *mgo.Session
var asession *mgo.Session
var collection *mgo.Collection
var serr error

var hosts string
var db string

var insightskey = "23c6b1ec-ca92-4083-86b6-eba851af9032"

var rabbitMQURL = os.Getenv("RABBITMQHOST")
var partitionKey = "0"
var mongoURL = os.Getenv("MONGOHOST")
var teamname = os.Getenv("TEAMNAME")
var eventPolicyName = os.Getenv("EVENTPOLICYNAME")
var eventPolicyKey = os.Getenv("EVENTPOLICYKEY")

var eventURL = os.Getenv("EVENTURL")

var eventURLWithPartition = os.Getenv("EVENTURL") + "/partitions/"

// AMQP
var ehSender eventhub.Sender

var (
	ehNamespace = os.Getenv("EH_TEST_NAMESPACE")
	ehName      = os.Getenv("EH_TEST_NAME")
)

// Order represents the order json
type Order struct {
	ID                string  `required:"false" description:"CosmoDB ID - will be autogenerated"`
	EmailAddress      string  `required:"true" description:"Email address of the customer"`
	PreferredLanguage string  `required:"false" description:"Preferred Language of the customer"`
	Product           string  `required:"false" description:"Product ordered by the customer"`
	Total             float64 `required:"false" description:"Order total"`
	Source            string  `required:"false" description:"Source backend e.g. App Service, Container instance, K8 cluster etc"`
	Status            string  `required:"true" description:"Order Status"`
}

func init() {

	OrderList = make(map[string]*Order)

	//Now we check if this mongo or cosmos
	if strings.Contains(mongoURL, "?ssl=true") {
		isAzure = true

		url, err := url.Parse(mongoURL)
		if err != nil {
			log.Fatal("Problem parsing url: ", err)
		}

		log.Print("user ", url.User)
		// DialInfo holds options for establishing a session with a MongoDB cluster.
		st := fmt.Sprintf("%s", url.User)
		co := strings.Index(st, ":")

		database = st[:co]
		password = st[co+1:]
		log.Print("db ", database, " pwd ", password)
	}

	// DialInfo holds options for establishing a session with a MongoDB cluster.
	dialInfo := &mgo.DialInfo{
		Addrs:    []string{fmt.Sprintf("%s.documents.azure.com:10255", database)}, // Get HOST + PORT
		Timeout:  60 * time.Second,
		Database: database, // It can be anything
		Username: database, // Username
		Password: password, // PASSWORD
		DialServer: func(addr *mgo.ServerAddr) (net.Conn, error) {
			return tls.Dial("tcp", addr.String(), &tls.Config{})
		},
	}

	// Create a session which maintains a pool of socket connections
	// to our MongoDB.
	if isAzure == true {
		asession, serr = mgo.DialWithInfo(dialInfo)
		if serr != nil {
			log.Fatal("Can't connect to CosmosDB, go error", serr)
			status = "Can't connect to CosmosDB, go error %v\n"
			os.Exit(1)
		}
		session = asession.Copy()
		log.Println("Writing to CosmosDB")
		db = "CosmosDB"
	} else {
		asession, serr = mgo.Dial(mongoURL)
		if serr != nil {
			log.Fatal("Can't connect to mongo, go error", serr)
			status = "Can't connect to mongo, go error %v\n"
			os.Exit(1)
		}
		session = asession.Copy()
		log.Println("Writing to MongoDB")
		db = "MongoDB"
	}

	// SetSafe changes the session safety mode.
	// If the safe parameter is nil, the session is put in unsafe mode, and writes become fire-and-forget,
	// without error checking. The unsafe mode is faster since operations won't hold on waiting for a confirmation.
	// http://godoc.org/labix.org/v2/mgo#Session.SetMode.
	session.SetSafe(nil)

	// get collection
	collection = session.DB(database).C("orders")

	// Now let's parse the eventhub
	url, err := url.Parse(eventURL)
	if err != nil {
		log.Fatal("Problem parsing url: ", err)
	}
	// Get the namespace
	ht := fmt.Sprintf("%s", url.Host)
	ho := strings.Index(ht, ".")
	ehNamespace = ht[:ho]

	// Get the eventhub
	et := fmt.Sprintf("%s", url.Path)
	ehName = et[1:]
	log.Print("namespace:", ehNamespace, "eventhubname:", ehName)

	ehSender, serr = eventhub.NewSender(eventhub.SenderOpts{
		EventHubNamespace:   ehNamespace,
		EventHubName:        ehName,
		SasPolicyName:       eventPolicyName,
		SasPolicyKey:        eventPolicyKey,
		TokenExpiryInterval: 20 * time.Second,
		Debug:               false,
	})
	if serr != nil {
		panic(serr)
	}
	//	defer ehSender.Close()

}

func AddOrder(order Order) (orderId string) {

	return orderId
}

// AddOrderToMongoDB Add the order to MondoDB
func AddOrderToMongoDB(order Order) (orderId string) {

	session = asession.Copy()

	log.Println("Team " + teamname)

	if partitionKey == "" {
		partitionKey = "0"
	}

	/* 	if order.Status == "Kill" {
		log.Println("Killing")
	} */

	NewOrderID := bson.NewObjectId()

	order.ID = NewOrderID.Hex()

	order.Status = "Open"
	if order.Source == "" || order.Source == "string" {
		order.Source = os.Getenv("SOURCE")
	}

	database = "k8orders"
	password = "" //V2

	log.Print(mongoURL, isAzure, "AMQP")

	//defer asession.Close()
	defer session.Close()
	// insert Document in collection
	serr = collection.Insert(order)
	log.Println("_id:", order)

	if serr != nil {
		log.Fatal("Problem inserting data: ", serr)
		log.Println("_id:", order)
	}

	//	Let's write only if we have a key
	if insightskey != "" {
		client := appinsights.NewTelemetryClient(insightskey)
		client.TrackEvent("CapureOrder: - Team Name " + teamname + " db " + db)
	}

	// Now let's place this on the eventhub
	if eventURL != "" {
		log.Println("Sending to event hub " + eventURLWithPartition)
		start := time.Now()
		r := new(big.Int)
		fmt.Println(r.Binomial(1000, 10))

		//	AddOrderToEventHub(order.ID, teamname)
		AddOrderToEventHubAMQP(order.ID, teamname)
		elapsed := time.Since(start)
		log.Printf("AMQP took %s", elapsed)

	} else {
		// Let's send to RabbitMQ
		log.Println("Sending to rabbitmq " + rabbitMQURL)
		AddOrderToRabbitMQ(order.ID, teamname)
	}

	return order.ID
}

// AddOrder to RabbitMQ

func AddOrderToRabbitMQ(orderId string, orderSource string) {

	//Instantiate RabbitMq
	conn, err := amqp.Dial(rabbitMQURL)
	failOnError(err, "Failed to connect to RabbitMQ")
	//	defer conn.Close()

	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")
	//	defer ch.Close()

	q, err := ch.QueueDeclare(
		"order", // name
		true,    // durable
		false,   // delete when unused
		false,   // exclusive
		false,   // no-wait
		nil,     // arguments
	)
	failOnError(err, "Failed to declare a queue")

	body := "{'order':" + "'" + orderId + "', 'source':" + "'" + orderSource + "'}"
	err = ch.Publish(
		"",     // exchange
		q.Name, // routing key
		false,  // mandatory
		false,  // immediate
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "application/json",
			Body:         []byte(body),
		})
	log.Printf(" [x] Sent %s " + body + " queue:" + q.Name)
	failOnError(err, "Failed to publish a message")
}

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
		panic(fmt.Sprintf("%s: %s", msg, err))
	}
}

// AddOrderToEventHub adds it to an event hub
func AddOrderToEventHub(orderId string, orderSource string) {

	rand.Seed(time.Now().UnixNano())
	partitionKey := strconv.Itoa(random(0, 3))
	eventURLWithPartition = os.Getenv("EVENTURL") + "/partitions/" + partitionKey + "/messages"

	log.Println("SaS pre ", eventURLWithPartition, eventPolicyName, eventPolicyKey)
	SaS := createSharedAccessToken(strings.TrimSpace(eventURLWithPartition), strings.TrimSpace(eventPolicyName), strings.TrimSpace(eventPolicyKey))
	log.Println("SaS post ", SaS)

	log.Println("evenurlwith partition  ", eventURLWithPartition)

	tr := &http.Transport{DisableKeepAlives: false}
	req, _ := http.NewRequest("POST", eventURLWithPartition, strings.NewReader("{'order':"+"'"+orderId+"', 'source':"+"'"+orderSource+"'}"))
	req.Header.Set("Authorization", SaS)
	req.Close = false

	res, err := tr.RoundTrip(req)
	if err != nil {
		fmt.Println(res, err)
	}

}

func createSharedAccessToken(uri string, saName string, saKey string) string {

	if len(uri) == 0 || len(saName) == 0 || len(saKey) == 0 {
		return "Missing required parameter"
	}

	encoded := template.URLQueryEscaper(uri)
	now := time.Now().Unix()
	week := 60 * 60 * 24 * 7
	ts := now + int64(week)
	signature := encoded + "\n" + strconv.Itoa(int(ts))
	key := []byte(saKey)
	hmac := hmac.New(sha256.New, key)
	hmac.Write([]byte(signature))
	hmacString := template.URLQueryEscaper(base64.StdEncoding.EncodeToString(hmac.Sum(nil)))

	result := "SharedAccessSignature sr=" + encoded + "&sig=" +
		hmacString + "&se=" + strconv.Itoa(int(ts)) + "&skn=" + saName
	return result
}

func random(min int, max int) int {
	return rand.Intn(max-min) + min
}

// AddOrderToEventHubAMQP adds it to an event hub
func AddOrderToEventHubAMQP(orderId string, orderSource string) {

	/*
		go func(s eventhub.Sender) {
			log.Println("Setting up the error channel...\n")
			for err := range s.ErrorChan() {
				if err != nil {
					log.Println("Just received an error: '%v'\n", err)
					panic(err)
				}
			}
		}(ehSender)
	*/
	log.Println("Now sending the order!\n")

	// Send Async
	// ------------------------------------
	uniqueID := ehSender.SendAsync("{'order':" + "'" + orderId + "', 'source':" + "'" + orderSource + "'}")
	idstring := strconv.FormatInt(int64(uniqueID), 10)
	log.Println("The order sent", idstring)

}
