package main

import (
	"context"
	"log"
	"math"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/google/uuid"
	clientutil "github.com/yosupo06/library-checker-judge/api/clientutil"
	pb "github.com/yosupo06/library-checker-judge/api/proto"
	"github.com/yosupo06/library-checker-judge/database"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gorm.io/gorm"
)

func TestProblemInfo(t *testing.T) {
	client := createTestAPIClientWithSetup(t, func(db *gorm.DB, authClient *DummyAuthClient) {
		database.SaveProblem(db, DUMMY_PROBLEM)
	})

	ctx := context.Background()

	problem, err := client.ProblemInfo(ctx, &pb.ProblemInfoRequest{
		Name: DUMMY_PROBLEM.Name,
	})
	if err != nil {
		t.Fatal(err)
	}
	if problem.Title != DUMMY_PROBLEM.Title {
		t.Fatal("Differ Title:", problem.Title)
	}
	if problem.SourceUrl != DUMMY_PROBLEM.SourceUrl {
		t.Fatal("Differ SourceURL:", problem.SourceUrl)
	}
	if math.Abs(problem.TimeLimit-2.0) > 0.01 {
		t.Fatal("Differ TimeLimit:", problem.TimeLimit)
	}
	if problem.TestcasesVersion != DUMMY_PROBLEM.TestCasesVersion {
		t.Fatal("Differ TestcasesVersion:", problem.TestcasesVersion)
	}
	if problem.Version != DUMMY_PROBLEM.Version {
		t.Fatal("Differ Version:", problem.Version)
	}
}

func TestNoExistProblemInfo(t *testing.T) {
	client := createTestAPIClient(t)

	ctx := context.Background()

	_, err := client.ProblemInfo(ctx, &pb.ProblemInfoRequest{
		Name: "This-problem-is-not-found",
	})
	if err == nil {
		t.Fatal("error is nil")
	}
	t.Log(err)
}

func TestLangList(t *testing.T) {
	client := createTestAPIClient(t)

	ctx := context.Background()
	list, err := client.LangList(ctx, &pb.LangListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Langs) == 0 {
		t.Fatal(err)
	}
}

func TestSubmissionSortOrderList(t *testing.T) {
	client := createTestAPIClient(t)

	ctx := context.Background()
	for _, order := range []string{"", "-id", "+time"} {
		_, err := client.SubmissionList(ctx, &pb.SubmissionListRequest{
			Skip:  0,
			Limit: 100,
			Order: order,
		})
		if err != nil {
			t.Fatal("Failed SubmissionList Order:", order)
		}
	}
	_, err := client.SubmissionList(ctx, &pb.SubmissionListRequest{
		Skip:  0,
		Limit: 100,
		Order: "dummy",
	})
	if err == nil {
		t.Fatal("Success SubmissionList Dummy Order")
	}
	t.Log(err)
}

func TestSubmitBig(t *testing.T) {
	client := createTestAPIClient(t)

	ctx := context.Background()
	bigSrc := strings.Repeat("a", 3*1000*1000) // 3 MB
	_, err := client.Submit(ctx, &pb.SubmitRequest{
		Problem: "aplusb",
		Source:  bigSrc,
		Lang:    "cpp",
	})
	if err == nil {
		t.Fatal("Success to submit big source")
	}
	t.Log(err)
}

func TestAnonymousRejudge(t *testing.T) {
	client := createTestAPIClientWithSetup(t, func(db *gorm.DB, authClient *DummyAuthClient) {
		database.SaveProblem(db, DUMMY_PROBLEM)
	})

	ctx := context.Background()
	src := strings.Repeat("a", 1000)
	resp, err := client.Submit(ctx, &pb.SubmitRequest{
		Problem: DUMMY_PROBLEM.Name,
		Source:  src,
		Lang:    "cpp",
	})
	if err != nil {
		t.Fatal("Unsuccess to submit source:", err)
	}
	_, err = client.Rejudge(ctx, &pb.RejudgeRequest{
		Id: resp.Id,
	})
	if err == nil {
		t.Fatal("Success to rejudge")
	}
}

func TestChangeNoExistUserInfo(t *testing.T) {
	client := createTestAPIClient(t)

	ctx := context.Background()
	_, err := client.ChangeUserInfo(ctx, &pb.ChangeUserInfoRequest{
		User: &pb.User{
			Name: "this_is_dummy_user_name",
		},
	})
	if err == nil {
		t.Fatal("Success to change unknown user")
	}
	t.Log(err)
}

func createTestAPIClientWithSetup(t *testing.T, setUp func(db *gorm.DB, authClient *DummyAuthClient)) pb.LibraryCheckerServiceClient {
	// launch gRPC server
	listen, err := net.Listen("tcp", ":50053")
	if err != nil {
		t.Fatal(err)
	}

	// connect database
	db := createTestDB(t)

	// connect authClient
	authClient := &DummyAuthClient{}

	s := NewGRPCServer(db, authClient, "../langs/langs.toml")
	go func() {
		if err := s.Serve(listen); err != nil {
			log.Fatal("Server exited: ", err)
		}
	}()

	options := []grpc.DialOption{grpc.WithBlock(), grpc.WithPerRPCCredentials(&clientutil.LoginCreds{}), grpc.WithTransportCredentials(insecure.NewCredentials())}
	conn, err := grpc.DialContext(
		context.Background(),
		"localhost:50053",
		options...,
	)
	if err != nil {
		t.Fatal(err)
	}

	setUp(db, authClient)

	t.Cleanup(func() {
		conn.Close()
		s.Stop()
	})

	return pb.NewLibraryCheckerServiceClient(conn)
}

func createTestAPIClient(t *testing.T) pb.LibraryCheckerServiceClient {
	return createTestAPIClientWithSetup(t, func(db *gorm.DB, authClient *DummyAuthClient) {})
}

func createTestDB(t *testing.T) *gorm.DB {
	dbName := uuid.New().String()
	t.Log("create DB:", dbName)

	createCmd := exec.Command("createdb",
		"-h", "localhost",
		"-U", "postgres",
		"-p", "5432",
		dbName)
	createCmd.Env = append(os.Environ(), "PGPASSWORD=passwd")
	if err := createCmd.Run(); err != nil {
		t.Fatal("createdb failed: ", err.Error())
	}

	db := database.Connect("localhost", "5432", dbName, "postgres", "passwd", getEnv("API_DB_LOG", "") != "")

	t.Cleanup(func() {
		db2, err := db.DB()
		if err != nil {
			t.Fatal("db.DB() failed:", err)
		}
		if err := db2.Close(); err != nil {
			t.Fatal("db.Close() failed:", err)
		}
		createCmd := exec.Command("dropdb",
			"-h", "localhost",
			"-U", "postgres",
			"-p", "5432",
			dbName)
		createCmd.Env = append(os.Environ(), "PGPASSWORD=passwd")
		createCmd.Stderr = os.Stderr
		createCmd.Stdin = os.Stdin
		if err := createCmd.Run(); err != nil {
			t.Fatal("dropdb failed:", err)
		}
	})

	return db
}

type DummyAuthClient struct {
	tokenToUID map[string]string
}

func (c *DummyAuthClient) parseUID(ctx context.Context, token string) string {
	return c.tokenToUID[token]
}

func (c *DummyAuthClient) registerUID(ctx context.Context, token string, uid string) {
	c.tokenToUID[token] = uid
}

var DUMMY_PROBLEM = database.Problem{
	Name:             "aplusb",
	Title:            "A + B",
	Statement:        "Please calculate A + B",
	Timelimit:        2000,
	TestCasesVersion: "dummy-testcase-version",
	Version:          "dummy-version",
	SourceUrl:        "https://github.com/yosupo06/library-checker-problems/tree/master/sample/aplusb",
}
