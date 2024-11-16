package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sso/internal/domain/models"
	jwtlocal "sso/internal/lib"
	"sso/internal/services/storage"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserExists         = errors.New("user already exist")
	ErrUserNotFound       = errors.New("user not found")
	ErrAppExist           = errors.New("app already exist")
	ErrInvalidRoles       = errors.New("invalid roles")
)

type Auth struct {
	log         *slog.Logger
	usrSaver    UserSaver
	usrProvider UserProvider
	appProvider AppProvider
	appSaver    AppSaver
	tokenTTL    time.Duration
}

type UserSaver interface {
	SaveUser(ctx context.Context, email string, passHash []byte) (uid int64, err error)
}

type UserProvider interface {
	DeleteUser(ctx context.Context, email string) (err error)
	User(ctx context.Context, email string) (modelU models.User, err error)
	SetRoles(ctx context.Context, email string, roles []string) (err error)
	GetRoles(ctx context.Context, email string) (roles []string, err error)
}

type AppSaver interface {
	SaveApp(ctx context.Context, name string, secret string) (appId int64, err error)
}

type AppProvider interface {
	App(ctx context.Context, appID int64) (modelA models.App, err error)
}

// New returns a new object of the Auth struct
func NewAuth(log *slog.Logger, usrSaver UserSaver,
	usrProvider UserProvider, appProvider AppProvider,
	appSaver AppSaver, tokenTTL time.Duration) *Auth {
	return &Auth{
		log:         log,
		usrSaver:    usrSaver,
		usrProvider: usrProvider,
		appProvider: appProvider,
		appSaver:    appSaver,
		tokenTTL:    tokenTTL,
	}
}

func (a *Auth) Login(ctx context.Context,
	email string, password string, appID int64) (string, error) {
	const op = "auth.Login"

	log := a.log.With(
		slog.String("op", op),
		slog.String("email", email),
	)

	log.Info("attempting to login user")

	user, err := a.usrProvider.User(ctx, email)
	if err != nil {
		if errors.Is(err, storage.ErrUserNotFound) {
			log.Error("not corrected login/password 1")
			return "", fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
		}
		log.Error("failed to get user")
		return "", fmt.Errorf("%s: %w", op, err)
	}

	if err := bcrypt.CompareHashAndPassword(user.PassHash, []byte(password)); err != nil {
		log.Error(string(user.PassHash))
		return "", fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
	}

	app, err := a.appProvider.App(ctx, appID)
	if err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}

	log.Info("successfully login user")

	token, err := jwtlocal.NewToken(user, app, a.tokenTTL)
	if err != nil {
		log.Error("cannot generate token")
		return "", fmt.Errorf("%s: %w", op, err)
	}

	return token, nil
}

func (a *Auth) RegisterNewUser(ctx context.Context, email string, password string) (int64, error) {
	const op = "auth.RegisterNewUser"

	log := a.log.With(
		slog.String("op", op),
		slog.String("email", email),
	)

	log.Info("registering new user")

	passHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Error("failed to generate password hash")
		return 0, fmt.Errorf("%s: %w", op, err)
	}

	id, err := a.usrSaver.SaveUser(ctx, email, passHash)
	if err != nil {
		if errors.Is(err, storage.ErrUserExist) {
			log.Error("user already exist")
			return 0, fmt.Errorf("%s: %w", op, ErrUserExists)
		}
		log.Error("failed to save user: " + err.Error())
		return 0, fmt.Errorf("%s: %w", op, err)
	}

	log.Info("successfully register user")

	return id, nil
}

/* func (a *Auth) IsAdmin(ctx context.Context, userId int64) (bool, error) {
	const op = "auth.IsAdmin"

	log := slog.With(slog.String("op", op), slog.Int64("userId", userId))

	result, err := a.usrProvider.IsAdmin(ctx, userId)
	if err != nil {
		if errors.Is(err, storage.ErrAppNotFound) {
			log.Error("user not found")
			return false, fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
		}
		log.Error("error when searching for user")
		return false, fmt.Errorf("%s: %w", op, err)
	}

	log.Info("success checking is admin", slog.Bool("isAdmin", result))

	return result, nil
} */

func (a *Auth) CreateApp(ctx context.Context, name string, secret string) (int64, error) {
	const op = "auth.NewApp"

	log := slog.With(slog.String("op", op), slog.String("username", name))

	appId, err := a.appSaver.SaveApp(ctx, name, secret)
	if err != nil {
		if errors.Is(err, storage.ErrAppExist) {
			log.Error("app already exist")
			return 0, fmt.Errorf("%s: %w", op, ErrAppExist)
		}
		log.Error("error adding a new app to the database: " + err.Error())
		return 0, fmt.Errorf("%s: %w", op, err)
	}

	log.Info("success create new app", slog.String("name", name))

	return appId, nil
}

func (a *Auth) SetRoles(ctx context.Context, email string, roles []string) error {
	const op = "auth.SetRoles"

	log := a.log.With(
		slog.String("op", op),
		slog.String("email", email),
	)

	for _, role := range roles {
		if role != "admin" && role != "user" && role != "manager" {
			log.Error("invalid role")
			return fmt.Errorf("%s: %s", op, ErrInvalidRoles)
		}
	}

	err := a.usrProvider.SetRoles(ctx, email, roles)
	if err != nil {
		log.Error("failed to set roles")
		if errors.Is(err, storage.ErrUserNotFound) {
			return fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
		}
		return fmt.Errorf("%s: %w", op, err)
	}

	log.Info("successfully set roles for user")
	return nil
}

func (a *Auth) GetRoles(ctx context.Context, email string) ([]string, error) {
	const op = "auth.GetRoles"

	log := a.log.With(
		slog.String("op", op),
		slog.String("email", email),
	)

	roles, err := a.usrProvider.GetRoles(ctx, email)
	if err != nil {
		log.Error("failed to get roles")
		if errors.Is(err, storage.ErrUserNotFound) {
			return nil, fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
		}
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	log.Info("successfully got roles for user")
	return roles, nil
}

func (a *Auth) DeleteUser(ctx context.Context, email string) error {
	const op = "auth.GetRoles"

	log := a.log.With(
		slog.String("op", op),
		slog.String("email", email),
	)

	err := a.usrProvider.DeleteUser(ctx, email)
	if err != nil {
		log.Error("failed delete user")
		if errors.Is(err, storage.ErrUserNotFound) {
			return fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
		}
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}
