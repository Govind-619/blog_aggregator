package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Govind-619/blog_aggregator/internal/database"
)

/* ---------- USER COMMANDS ---------- */

func handlerLogin(s *state, cmd command) error {
	if len(cmd.args) < 1 {
		return fmt.Errorf("login requires a username")
	}

	username := cmd.args[0]

	_, err := s.db.GetUser(context.Background(), username)
	if err != nil {
		return fmt.Errorf("user does not exist")
	}

	return s.cfg.SetUser(username)
}

func handlerRegister(s *state, cmd command) error {
	if len(cmd.args) < 1 {
		return fmt.Errorf("register requires a username")
	}

	user, err := s.db.CreateUser(
		context.Background(),
		database.CreateUserParams{
			ID:        uuid.New(),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Name:      cmd.args[0],
		},
	)
	if err != nil {
		return fmt.Errorf("user already exists")
	}

	if err := s.cfg.SetUser(user.Name); err != nil {
		return err
	}

	log.Printf("user created: %+v\n", user)
	return nil
}

func handlerReset(s *state, cmd command) error {
	return s.db.ResetUsers(context.Background())
}

func handlerUsers(s *state, cmd command) error {
	users, err := s.db.GetUsers(context.Background())
	if err != nil {
		return err
	}

	for _, u := range users {
		if u.Name == s.cfg.CurrentUserName {
			fmt.Printf("* %s (current)\n", u.Name)
		} else {
			fmt.Printf("* %s\n", u.Name)
		}
	}
	return nil
}

/* ---------- FEEDS ---------- */

func handlerAddFeed(s *state, cmd command, user database.User) error {
	if len(cmd.args) != 2 {
		return fmt.Errorf("usage: addfeed <name> <url>")
	}

	feed, err := s.db.CreateFeed(
		context.Background(),
		database.CreateFeedParams{
			Name:   cmd.args[0],
			Url:    cmd.args[1],
			UserID: user.ID,
		},
	)
	if err != nil {
		return err
	}

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
	return err
}

func handlerFeeds(s *state, cmd command) error {
	feeds, err := s.db.GetFeeds(context.Background())
	if err != nil {
		return err
	}

	for _, f := range feeds {
		fmt.Printf("* %s (%s) by %s\n", f.Name, f.Url, f.UserName)
	}
	return nil
}

/* ---------- FOLLOWING ---------- */

func handlerFollow(s *state, cmd command) error {
	if len(cmd.args) < 1 {
		return fmt.Errorf("follow requires a feed url")
	}

	user, _ := s.db.GetUser(context.Background(), s.cfg.CurrentUserName)
	feed, _ := s.db.GetFeedByUrl(context.Background(), cmd.args[0])

	_, err := s.db.CreateFeedFollow(
		context.Background(),
		database.CreateFeedFollowParams{
			ID:        uuid.New(),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			UserID:    user.ID,
			FeedID:    feed.ID,
		},
	)
	return err
}

func handlerUnfollow(s *state, cmd command, user database.User) error {
	feed, err := s.db.GetFeedByUrl(context.Background(), cmd.args[0])
	if err != nil {
		return err
	}

	return s.db.DeleteFeedFollow(
		context.Background(),
		database.DeleteFeedFollowParams{
			UserID: user.ID,
			FeedID: feed.ID,
		},
	)
}

/* ---------- AGGREGATION ---------- */

func handlerAgg(s *state, cmd command) error {
	d, err := time.ParseDuration(cmd.args[0])
	if err != nil {
		return err
	}

	ticker := time.NewTicker(d)
	for ; ; <-ticker.C {
		scrapeFeeds(s)
	}
}

func scrapeFeeds(s *state) {
	ctx := context.Background()

	feedRow, err := s.db.GetNextFeedToFetch(ctx)
	if err != nil {
		return
	}

	_ = s.db.MarkFeedFetched(ctx, feedRow.ID)

	feed, err := fetchFeed(ctx, feedRow.Url)
	if err != nil {
		return
	}

	for _, item := range feed.Channel.Items {
		var publishedAt sql.NullTime
		if item.PubDate != "" {
			if t, err := ParsePublishedAt(item.PubDate); err == nil {
				publishedAt = sql.NullTime{Time: t, Valid: true}
			}
		}

		_, err := s.db.CreatePost(ctx, database.CreatePostParams{
			Title: item.Title,
			Url:   item.Link,
			Description: sql.NullString{
				String: item.Description,
				Valid:  item.Description != "",
			},
			PublishedAt: publishedAt,
			FeedID:      feedRow.ID,
		})

		if err != nil && !strings.Contains(err.Error(), "duplicate") {
			log.Println(err)
		}
	}
}

/* ---------- BROWSE ---------- */

func handlerBrowse(s *state, cmd command, user database.User) error {
	limit := int32(2)
	if len(cmd.args) > 0 {
		l, _ := strconv.Atoi(cmd.args[0])
		limit = int32(l)
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

	for _, p := range posts {
		fmt.Println(p.Title)
		fmt.Println(p.Url)
		fmt.Println("----")
	}
	return nil
}

func handlerFollowing(s *state, cmd command) error {
	user, err := s.db.GetUser(context.Background(), s.cfg.CurrentUserName)
	if err != nil {
		return err
	}

	follows, err := s.db.GetFeedFollowsForUser(context.Background(), user.ID)
	if err != nil {
		return err
	}

	for _, follow := range follows {
		fmt.Printf("* %s\n", follow.FeedName)
	}

	return nil
}

func ParsePublishedAt(s string) (time.Time, error) {
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
