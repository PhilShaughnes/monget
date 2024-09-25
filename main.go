package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	MONGO_PREFIX = "mongodb://"
	MONGO_OPTS   = "?tls=true&replicaSet=rs0&readPreference=secondaryPreferred&retryWrites=false"
	ORGS         = "organizations"
	USERS        = "users"
	dbTimeout    = 5 * time.Second
)

type Org struct {
	ID         string `bson:"id"`
	Path       string `bson:"path"`
	CustomerID string `bson:"customerId"`
}

type UserOrganization struct {
	UserID string `bson:"userId"`
	OrgID  string `bson:"orgId"`
	Email  string `bson:"email"`
}

type User struct {
	ExternalID    string             `bson:"external_id"`
	Organizations []UserOrganization `bson:"organizations"`
}

type MDB struct {
	db  *mongo.Database
	ctx *context.Context
}

func main() {
	var (
		murl    = flag.String("murl", "mongodb://localhost", "mongo url")
		mdb     = flag.String("mdb", "idm", "mongo db")
		mport   = flag.Int("mport", 27017, "mongo port")
		org     = flag.String("org", "testorg", "org to search")
		outFile = flag.String("file", "", "output file path (leave empty for stdout)")
	)
	flag.Parse()
	if !strings.HasPrefix(*murl, MONGO_PREFIX) {
		*murl = MONGO_PREFIX + *murl
	}
	mongoUrl := fmt.Sprintf("%s:%d/%s", *murl, *mport, MONGO_OPTS)

	opts := options.Client().ApplyURI(mongoUrl)
	err := opts.Validate()
	if err != nil {
		slog.Error("got err", slog.Any("err", err))
		os.Exit(1)
	}
	opts.TLSConfig.InsecureSkipVerify = true
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		slog.Error("couldn't connect: %e", slog.Any("err", err))
		os.Exit(1)
	}
	defer client.Disconnect(ctx)

	db := client.Database(*mdb)
	m := &MDB{
		db:  db,
		ctx: &ctx,
	}

	orgId, err := m.getOrgId(*org)
	if err != nil {
		slog.Error("couldn't get org id: %e", slog.Any("err", err))
		os.Exit(1)
	}
	users, err := m.getUsers(orgId)
	if err != nil {
		slog.Error(
			"couldn't get users for org ",
			slog.String("org", *org),
			slog.String("orgId", orgId),
			slog.Any("err", err),
		)
		os.Exit(1)
	}

	output := os.Stdout
	if *outFile != "" {
		file, err := os.Create(*outFile)
		if err != nil {
			slog.Error(
				"err creating file %s: %e",
				slog.String("filename", *outFile),
				slog.Any("err", err),
			)
			os.Exit(1)
		}
		defer file.Close()
		output = file
	}

	usersJSON, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		slog.Error("err converting to json: %e", slog.Any("err", err))
		fmt.Println(users)
		return
	}
	fmt.Fprintln(output, string(usersJSON))
}

func (m *MDB) getOrgId(orgPath string) (string, error) {
	var org Org
	orgs := m.db.Collection(ORGS)
	err := orgs.FindOne(*m.ctx, bson.M{"path": orgPath}).Decode(&org)
	if err != nil {
		return "", err
	}
	return org.ID, nil
}

func (m *MDB) getUsers(orgId string) ([]User, error) {
	var resUsers []User
	users := m.db.Collection(USERS)
	filter := bson.M{
		"organizations.orgId": orgId,
	}

	cursor, err := users.Find(*m.ctx, filter)
	if err != nil {
		return resUsers, err
	}
	defer cursor.Close(*m.ctx)

	if err = cursor.All(*m.ctx, &resUsers); err != nil {
		return resUsers, err
	}
	return resUsers, nil
}
