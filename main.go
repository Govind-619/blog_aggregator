package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"github.com/Govind-619/blog_aggregator/internal/config"
	"github.com/Govind-619/blog_aggregator/internal/database"
)

type state struct {
	db  *database.Queries
	cfg *config.Config
}

type command struct {
	name string
	args []string
}

type commands struct {
	m map[string]func(*state, command) error
}

type GetFeedsRow struct {
	ID        uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
	Name      string
	Url       string
	UserID    uuid.UUID
	UserName  string
}

func (c *commands) register(
	name string,
	f func(*state, command) error,
) {
	c.m[name] = f
}

func (c *commands) run(s *state, cmd command) error {
	handler, ok := c.m[cmd.name]
	if !ok {
		return fmt.Errorf("unknown command: %s", cmd.name)
	}
	return handler(s, cmd)
}

/* ---------------- HANDLERS ---------------- */

func handlerLogin(s *state, cmd command) error {
	if len(cmd.args) < 1 {
		fmt.Println("login requires a username")
		os.Exit(1)
	}

	username := cmd.args[0]

	_, err := s.db.GetUser(context.Background(), username)
	if err != nil {
		fmt.Println("user does not exist")
		os.Exit(1)
	}

	if err := s.cfg.SetUser(username); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Logged in as %s\n", username)
	return nil
}

func handlerRegister(s *state, cmd command) error {
	if len(cmd.args) < 1 {
		fmt.Println("register requires a username")
		os.Exit(1)
	}

	name := cmd.args[0]

	user, err := s.db.CreateUser(
		context.Background(),
		database.CreateUserParams{
			ID:        uuid.New(),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Name:      name,
		},
	)

	if err != nil {
		fmt.Println("user already exists")
		os.Exit(1)
	}

	if err := s.cfg.SetUser(name); err != nil {
		log.Fatal(err)
	}

	fmt.Println("User created")
	log.Printf("User: %+v\n", user)
	return nil
}

func handlerReset(s *state, cmd command) error {
	err := s.db.ResetUsers(context.Background())
	if err != nil {
		fmt.Println("failed to reset database")
		os.Exit(1)
	}

	fmt.Println("database reset successful")
	return nil
}

func handlerUsers(s *state, cmd command) error {
	users, err := s.db.GetUsers(context.Background())
	if err != nil {
		fmt.Println("failed to get users")
		os.Exit(1)
	}

	for _, user := range users {
		if user.Name == s.cfg.CurrentUserName {
			fmt.Printf("* %s (current)\n", user.Name)
		} else {
			fmt.Printf("* %s\n", user.Name)
		}
	}

	return nil
}

func handlerAddFeed(
	s *state,
	cmd command,
	user database.User,
) error {
	if len(cmd.args) != 2 {
		return fmt.Errorf("usage: addfeed <name> <url>")
	}

	name := cmd.args[0]
	url := cmd.args[1]

	feed, err := s.db.CreateFeed(
		context.Background(),
		database.CreateFeedParams{
			Name:   name,
			Url:    url,
			UserID: user.ID,
		},
	)
	if err != nil {
		return err
	}

	// auto-follow
	_, err = s.db.CreateFeedFollow(
		context.Background(),
		database.CreateFeedFollowParams{
			ID:        uuid.New(),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			UserID:    user.ID,
			FeedID:    feed.ID,
		},
	)
	if err != nil {
		return err
	}

	fmt.Println("Feed created:")
	fmt.Println("Name:", feed.Name)
	fmt.Println("URL:", feed.Url)

	return nil
}

func handlerFeeds(s *state, cmd command) error {
	feeds, err := s.db.GetFeeds(context.Background())
	if err != nil {
		return err
	}

	for _, feed := range feeds {
		fmt.Printf(
			"* %s (%s) by %s\n",
			feed.Name,
			feed.Url,
			feed.UserName,
		)
	}

	return nil
}

func handlerFollow(s *state, cmd command) error {
	if len(cmd.args) < 1 {
		return fmt.Errorf("follow requires a feed url")
	}

	feedURL := cmd.args[0]

	user, err := s.db.GetUser(context.Background(), s.cfg.CurrentUserName)
	if err != nil {
		return err
	}

	feed, err := s.db.GetFeedByUrl(context.Background(), feedURL)
	if err != nil {
		return err
	}

	follow, err := s.db.CreateFeedFollow(
		context.Background(),
		database.CreateFeedFollowParams{
			ID:        uuid.New(),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			UserID:    user.ID,
			FeedID:    feed.ID,
		},
	)
	if err != nil {
		return err
	}

	fmt.Printf(
		"%s is now following %s\n",
		follow.UserName,
		follow.FeedName,
	)

	return nil
}

func handlerFollowing(s *state, cmd command) error {
	user, err := s.db.GetUser(context.Background(), s.cfg.CurrentUserName)
	if err != nil {
		return err
	}

	follows, err := s.db.GetFeedFollowsForUser(
		context.Background(),
		user.ID,
	)
	if err != nil {
		return err
	}

	for _, follow := range follows {
		fmt.Printf("* %s\n", follow.FeedName)
	}

	return nil
}

func middlewareLoggedIn(
	handler func(s *state, cmd command, user database.User) error,
) func(*state, command) error {

	return func(s *state, cmd command) error {
		if s.cfg.CurrentUserName == "" {
			return fmt.Errorf("no user logged in")
		}

		user, err := s.db.GetUser(
			context.Background(),
			s.cfg.CurrentUserName,
		)
		if err != nil {
			return err
		}

		return handler(s, cmd, user)
	}
}

func handlerUnfollow(
	s *state,
	cmd command,
	user database.User,
) error {
	if len(cmd.args) < 1 {
		return fmt.Errorf("unfollow requires a feed url")
	}

	feedURL := cmd.args[0]

	feed, err := s.db.GetFeedByUrl(
		context.Background(),
		feedURL,
	)
	if err != nil {
		return err
	}

	err = s.db.DeleteFeedFollow(
		context.Background(),
		database.DeleteFeedFollowParams{
			UserID: user.ID,
			FeedID: feed.ID,
		},
	)
	if err != nil {
		return err
	}

	fmt.Printf(
		"%s unfollowed %s\n",
		user.Name,
		feed.Name,
	)

	return nil
}

func scrapeFeeds(s *state) {
	ctx := context.Background()

	feedRow, err := s.db.GetNextFeedToFetch(ctx)
	if err != nil {
		log.Printf("error getting next feed: %v", err)
		return
	}

	err = s.db.MarkFeedFetched(ctx, feedRow.ID)
	if err != nil {
		log.Printf("error marking feed fetched: %v", err)
		return
	}

	feed, err := fetchFeed(ctx, feedRow.Url)
	if err != nil {
		log.Printf("error fetching feed %s: %v", feedRow.Url, err)
		return
	}

	// ✅ YOUR CODE GOES HERE
	for _, item := range feed.Channel.Items {
		var publishedAt sql.NullTime
		if item.PubDate != "" {
			if t, err := parsePublishedAt(item.PubDate); err == nil {
				publishedAt = sql.NullTime{
					Time:  t,
					Valid: true,
				}
			}
		}

		_, err := s.db.CreatePost(
			ctx,
			database.CreatePostParams{
				Title: item.Title,
				Url:   item.Link,
				Description: sql.NullString{
					String: item.Description,
					Valid:  item.Description != "",
				},
				PublishedAt: publishedAt,
				FeedID:      feedRow.ID,
			},
		)

		if err != nil {
			if strings.Contains(err.Error(), "duplicate key") {
				continue
			}
			log.Printf("error creating post: %v", err)
		}
	}
}

func handlerAgg(s *state, cmd command) error {
	if len(cmd.args) < 1 {
		return fmt.Errorf("agg requires time_between_reqs")
	}

	timeBetweenRequests, err := time.ParseDuration(cmd.args[0])
	if err != nil {
		return err
	}

	fmt.Printf(
		"Collecting feeds every %s\n",
		timeBetweenRequests,
	)

	ticker := time.NewTicker(timeBetweenRequests)
	defer ticker.Stop()

	for ; ; <-ticker.C {
		scrapeFeeds(s)
	}
}

func parsePublishedAt(s string) (time.Time, error) {
	layouts := []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC3339,
		time.RFC822Z,
		time.RFC822,
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported date format: %s", s)

}

func handlerBrowse(s *state, cmd command, user database.User) error {
	limit := int32(2)

	if len(cmd.args) > 0 {
		parsed, err := strconv.Atoi(cmd.args[0])
		if err != nil {
			return fmt.Errorf("invalid limit")
		}
		limit = int32(parsed)
	}

	posts, err := s.db.GetPostsForUser(
		context.Background(),
		database.GetPostsForUserParams{
			UserID: user.ID,
			Limit:  limit,
		},
	)
	if err != nil {
		return err
	}

	for _, post := range posts {
		fmt.Println("Title:", post.Title)
		fmt.Println("URL:", post.Url)
		if post.PublishedAt.Valid {
			fmt.Println("Published:", post.PublishedAt.Time)
		}
		fmt.Println("----")
	}

	return nil
}

/* ---------------- MAIN ---------------- */

func main() {
	if len(os.Args) < 2 {
		fmt.Println("error: not enough arguments provided")
		os.Exit(1)
	}

	cfg, err := config.Read()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	db, err := sql.Open("postgres", cfg.DBURL)
	if err != nil {
		log.Fatal(err)
	}

	dbQueries := database.New(db)

	appState := &state{
		db:  dbQueries,
		cfg: &cfg,
	}

	cmds := &commands{
		m: make(map[string]func(*state, command) error),
	}

	cmds.register("login", handlerLogin)
	cmds.register("register", handlerRegister)
	cmds.register("reset", handlerReset)
	cmds.register("users", handlerUsers)
	cmds.register("agg", handlerAgg)
	cmds.register("addfeed", middlewareLoggedIn(handlerAddFeed))
	cmds.register("feeds", handlerFeeds)
	cmds.register("follow", handlerFollow)
	cmds.register("following", handlerFollowing)
	cmds.register("unfollow", middlewareLoggedIn(handlerUnfollow))
	cmds.register("agg", handlerAgg)
	cmds.register("browse", middlewareLoggedIn(handlerBrowse))

	cmd := command{
		name: os.Args[1],
		args: os.Args[2:],
	}

	if err := cmds.run(appState, cmd); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
