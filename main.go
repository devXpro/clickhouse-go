package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/iancoleman/strcase"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"io/ioutil"
	"log"
	"net/http"
	"reflect"
	"sort"
	"time"
)

var sqlConnect *sql.DB
var mongoConnect *mongo.Client
var games []string

const MONGO_HOST = "mongodb://mongodb:27017"

func initDB() {
	// Set client options
	clientOptions := options.Client().ApplyURI(fmt.Sprintf("%s", MONGO_HOST))
	// Connect to MongoDB
	var err error
	mongoConnect, err = mongo.Connect(context.Background(), clientOptions)
	handleError(err)
	//Check the connection
	err = mongoConnect.Ping(context.Background(), nil)
	handleError(err)
	fmt.Println(fmt.Sprintf("Connected to MongoDB on %s!", MONGO_HOST))
}

func initCollection(name string) {
	if !contains(games, name) {
		createCollection(name)
		games = append(games, name)
	}
}

func contains(s []string, searchterm string) bool {
	i := sort.SearchStrings(s, searchterm)
	return i < len(s) && s[i] == searchterm
}

func main() {
	initDB()

	handler := mux.NewRouter()
	handler.HandleFunc("/addEvent/{game}", Logger(addEvent))
	handler.HandleFunc("/", welcomePage)
	s := http.Server{
		Addr:    "0.0.0.0:1234",
		Handler: handler,
		//ReadTimeout:    1000 * time.Second,
		//WriteTimeout:   1000 * time.Second,
		//IdleTimeout:    0 * time.Second,
		//MaxHeaderBytes: 1 << 20, //1*2^20 - 128 kByte
	}
	log.Println(s.ListenAndServe())
}

func welcomePage(w http.ResponseWriter, r *http.Request) {
	renderResponse(nil, w,
		`<head>
<link rel="stylesheet" href="https://stackpath.bootstrapcdn.com/bootstrap/4.3.1/css/bootstrap.min.css" 
integrity="sha384-ggOyR0iXCbMQv3Xipma34MD+dH/1fQ784/j6cY/iJTQUOhcWr7x9JvoRxT2MZw1T" crossorigin="anonymous"></head>
					<div class="text-center">	<h2>Welcome to ClickHouse event tracker API</h2>
						<h5>Please read <a href="https://docs.google.com/document/d/1XsQY9_5Yi7YpYj_b6xQq4Qm7ePc7MNgWv5o-gL7Gjj8/edit?usp=sharing">documentation</a></h5>
					</div>`)
}

type Query struct {
	QueryType string `json:"query_type"`
	Query     string `json:"query"`
}

type LinuxTime int

type Event struct {
	PlayerId       string                 `json:"player_id"`
	EventType      string                 `json:"event_type"`
	EventData      map[string]interface{} `json:"event_data"`
	PlayerMetaData map[string]interface{} `json:"player_meta_data"`
	SessionUid     string                 `json:"session_uid"`
	DateTime       LinuxTime              `json:"date_time"`
	Registered     LinuxTime              `json:"registered"`
	AppVersion     string                 `json:"app_version"`
	PlayerLevel    int                    `json:"player_level"`
	ExpCount       int                    `json:"exp_count"`
	SessionNum     int                    `json:"session_num"`
	SoftBalance    int                    `json:"soft_balance"`
	HardBalance    int                    `json:"hard_balance"`
	StarsBalance   int                    `json:"stars_balance"`
	EnergyBalance  int                    `json:"energy_balance"`
	TrafficSource  string                 `json:"traffic_source"`
	AdCompany      string                 `json:"ad_company"`
	AdName         string                 `json:"ad_name"`
}

func createCollection(name string) {
	col := mongoConnect.Database("redash").Collection(name)
	createIndex("date_time", col)
	createIndex("registered", col)
}

func createIndex(name string, col *mongo.Collection) {
	mod := mongo.IndexModel{
		Keys: bson.M{
			name: -1, // index in ascending order
		}, Options: nil,
	}
	ind, err := col.Indexes().CreateOne(context.TODO(), mod)
	handleError(err)
	fmt.Println("CreateOne() index:", ind)
}

func addEvent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	game := vars["game"]
	initCollection(game)
	body, err := ioutil.ReadAll(r.Body)
	var event Event
	err = json.Unmarshal(body, &event)
	var stringResult string

	if err == nil {
		stringResult, err = insertEvent(event, game)
	}
	renderResponse(err, w, stringResult)
}

func insertEvent(event Event, game string) (string, error) {
	e := reflect.ValueOf(&event).Elem()
	doc := bson.M{}
	for i := 0; i < e.NumField(); i++ {
		varName := strcase.ToSnake(e.Type().Field(i).Name)
		varType := strcase.ToCamel(fmt.Sprintf("%v", e.Type().Field(i).Type))

		doc[strcase.ToCamel(varName)] = castValueByType(varType, e.Field(i).Interface(), false)
	}

	col := mongoConnect.Database("redash").Collection(game)
	result, insertErr := col.InsertOne(context.TODO(), doc)
	// Check for any insertion errors
	if insertErr != nil {
		fmt.Println("InsertOne ERROR: ", insertErr)
		return `{"error":" `+ insertErr.error +`"}`, nil
	} else {
		fmt.Println("InsertOne() API result:", result)
	}

	return `{"status":"ok"}`, nil
}

func castValueByType(structType string, value interface{}, mapAsString bool) interface{} {
	switch structType {
	case "MainLinuxTime":
		return time.Unix(int64(value.(LinuxTime)), 0)
	case "Mapstringinterface":
		if mapAsString {
			value, _ = json.Marshal(value)
		}
		return value
	default:
		return value
	}
}

func renderResponse(err error, w http.ResponseWriter, stringResult string) {
	err = getJsonError(err)
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		_, err = w.Write([]byte(err.Error()))
	} else {
		w.WriteHeader(http.StatusOK)
		_, err = w.Write([]byte(stringResult))
	}
}

func Logger(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		log.Printf("Server [http] method [%s] connnection from [%v]", r.Method, r.RemoteAddr)
		next.ServeHTTP(w, r)
	}
}

func getJsonError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(fmt.Sprintf(`{"error":"%s"}`, err.Error()))
}

func handleError(error error) {
	if error != nil {
		log.Fatal(error)
	}
}
