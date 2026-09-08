package postgres

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	URL             string
	ConnectTimeout  time.Duration
	QueryTimeout    time.Duration
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Transactor interface {
	WithinTransaction(context.Context, func(context.Context, DBTX) error) error
	WithinReadOnlyRepeatableRead(context.Context, func(context.Context, DBTX) error) error
}

type Pool struct {
	pool *pgxpool.Pool
}

type MigrationReadiness struct {
	pool            *Pool
	expectedVersion int64
}

func Open(ctx context.Context, config Config) (*Pool, error) {
	if config.URL == "" || config.ConnectTimeout <= 0 || config.QueryTimeout < time.Millisecond || config.MaxConns < 1 || config.MinConns < 0 || config.MinConns > config.MaxConns || config.MaxConnLifetime <= 0 || config.MaxConnIdleTime <= 0 {
		return nil, fmt.Errorf("invalid database pool configuration")
	}
	poolConfig, err := pgxpool.ParseConfig(config.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	poolConfig.ConnConfig.ConnectTimeout = config.ConnectTimeout
	poolConfig.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(config.QueryTimeout.Milliseconds(), 10)
	poolConfig.MaxConns = config.MaxConns
	poolConfig.MinConns = config.MinConns
	poolConfig.MaxConnLifetime = config.MaxConnLifetime
	poolConfig.MaxConnIdleTime = config.MaxConnIdleTime

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	connection := &Pool{pool: pool}
	if err := connection.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return connection, nil
}

func (p *Pool) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return p.pool.Exec(ctx, sql, arguments...)
}

func (p *Pool) Query(ctx context.Context, sql string, arguments ...any) (pgx.Rows, error) {
	return p.pool.Query(ctx, sql, arguments...)
}

func (p *Pool) QueryRow(ctx context.Context, sql string, arguments ...any) pgx.Row {
	return p.pool.QueryRow(ctx, sql, arguments...)
}

func (p *Pool) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }
func (p *Pool) Close()                         { p.pool.Close() }

func NewMigrationReadiness(pool *Pool, expectedVersion int64) *MigrationReadiness {
	return &MigrationReadiness{pool: pool, expectedVersion: expectedVersion}
}

func (readiness *MigrationReadiness) Ping(ctx context.Context) error {
	if err := readiness.pool.Ping(ctx); err != nil {
		return err
	}
	var version int64
	var dirty bool
	if err := readiness.pool.QueryRow(ctx, "SELECT version, dirty FROM schema_migrations LIMIT 1").Scan(&version, &dirty); err != nil {
		return fmt.Errorf("read migration state: %w", err)
	}
	if dirty || version != readiness.expectedVersion {
		return fmt.Errorf("unexpected migration state: version=%d dirty=%t, expected version=%d", version, dirty, readiness.expectedVersion)
	}
	return nil
}

func (p *Pool) WithinTransaction(ctx context.Context, operation func(context.Context, DBTX) error) error {
	return p.withinTransaction(ctx, pgx.TxOptions{}, operation)
}

func (p *Pool) WithinReadOnlyRepeatableRead(ctx context.Context, operation func(context.Context, DBTX) error) error {
	return p.withinTransaction(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	}, operation)
}

func (p *Pool) withinTransaction(ctx context.Context, options pgx.TxOptions, operation func(context.Context, DBTX) error) error {
	tx, err := p.pool.BeginTx(ctx, options)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := operation(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

var _ DBTX = (*Pool)(nil)
var _ Transactor = (*Pool)(nil)
var _ interface{ Ping(context.Context) error } = (*MigrationReadiness)(nil)
