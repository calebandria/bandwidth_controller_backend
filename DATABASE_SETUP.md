# PostgreSQL Database Setup Guide

## Overview
This application now uses PostgreSQL for user authentication and authorization. The database stores user credentials with bcrypt-hashed passwords for secure authentication.

## Database Schema

### Users Table
```sql
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(20) NOT NULL DEFAULT 'user',
    name VARCHAR(100) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

## Setup Instructions

### 1. Install PostgreSQL
```bash
# Ubuntu/Debian
sudo apt update
sudo apt install postgresql postgresql-contrib

# Start PostgreSQL service
sudo systemctl start postgresql
sudo systemctl enable postgresql
```

### 2. Create Database and User
```bash
# Switch to postgres user
sudo -u postgres psql

# In PostgreSQL shell, run:
CREATE DATABASE qos_bandwidth;
CREATE USER qos_user WITH PASSWORD 'qos_password';
GRANT ALL PRIVILEGES ON DATABASE qos_bandwidth TO qos_user;

# Connect to the new database
\c qos_bandwidth

# Grant schema privileges
GRANT ALL ON SCHEMA public TO qos_user;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO qos_user;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO qos_user;

# Exit
\q
```

### 3. Run Database Migrations
```bash
# Execute the migration script
cd /home/kaleba/qos_bandwidth/bandwidth_controller_backend
sudo -u postgres psql -d qos_bandwidth -f migrations/001_create_users_table.sql
```

### 4. Configure Environment Variables
Create a `.env` file or set environment variables:

```bash
export DB_HOST=localhost
export DB_PORT=5432
export DB_USER=qos_user
export DB_PASSWORD=qos_password
export DB_NAME=qos_bandwidth
export DB_SSLMODE=disable
```

Or create a `.env` file:
```env
DB_HOST=localhost
DB_PORT=5432
DB_USER=qos_user
DB_PASSWORD=qos_password
DB_NAME=qos_bandwidth
DB_SSLMODE=disable
```

## Default Users

The migration script creates two default users:

| Username | Password | Role  | Description          |
|----------|----------|-------|----------------------|
| admin    | admin123 | admin | Administrator account|
| user     | user123  | user  | Standard user account|

**⚠️ IMPORTANT: Change these default passwords in production!**

## Creating Password Hashes

To create a new user with a hashed password, you can use this Go snippet:

```go
package main

import (
    "fmt"
    "golang.org/x/crypto/bcrypt"
)

func main() {
    password := "your_password_here"
    hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    fmt.Println(string(hash))
}
```

Or use the PostgreSQL pgcrypto extension:
```sql
INSERT INTO users (username, password_hash, role, name)
VALUES ('newuser', crypt('newpassword', gen_salt('bf')), 'user', 'New User');
```

## Verifying Database Connection

```bash
# Test database connection
psql -h localhost -U qos_user -d qos_bandwidth -c "SELECT * FROM users;"

# Check tables
psql -h localhost -U qos_user -d qos_bandwidth -c "\dt"
```

## Application Configuration

The application automatically:
1. Tries to connect to PostgreSQL using environment variables
2. Falls back to in-memory auth repository if database connection fails
3. Logs connection status on startup

### Connection Log Messages
- ✅ `"Successfully connected to PostgreSQL database"` - Database is connected
- ⚠️ `"Warning: Could not connect to PostgreSQL"` - Falling back to in-memory storage

## Database Maintenance

### Backup Database
```bash
pg_dump -U qos_user -d qos_bandwidth > backup_$(date +%Y%m%d).sql
```

### Restore Database
```bash
psql -U qos_user -d qos_bandwidth < backup_20231217.sql
```

### View All Users
```bash
psql -U qos_user -d qos_bandwidth -c "SELECT id, username, role, name, created_at FROM users;"
```

### Reset a User's Password
```bash
# Generate a new hash using bcrypt (cost 10)
# Then update in database:
psql -U qos_user -d qos_bandwidth -c "UPDATE users SET password_hash = '$2a$10$NEW_HASH_HERE' WHERE username = 'admin';"
```

## Security Best Practices

1. **Change default passwords immediately**
2. **Use strong database passwords** (16+ characters, mixed case, numbers, symbols)
3. **Enable SSL/TLS** for database connections in production (set `DB_SSLMODE=require`)
4. **Restrict database access** to localhost or specific IPs
5. **Regular backups** of the database
6. **Monitor failed login attempts**
7. **Use separate database users** for different environments (dev, staging, prod)

## Troubleshooting

### Connection Refused
```bash
# Check if PostgreSQL is running
sudo systemctl status postgresql

# Check PostgreSQL is listening on correct port
sudo netstat -plnt | grep 5432
```

### Authentication Failed
```bash
# Edit pg_hba.conf to allow password authentication
sudo nano /etc/postgresql/*/main/pg_hba.conf

# Change 'peer' to 'md5' for local connections:
# local   all             all                                     md5

# Restart PostgreSQL
sudo systemctl restart postgresql
```

### Permission Denied
```bash
# Grant permissions to user
sudo -u postgres psql -d qos_bandwidth -c "GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO qos_user;"
sudo -u postgres psql -d qos_bandwidth -c "GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO qos_user;"
```

## Production Recommendations

1. Use a dedicated PostgreSQL server (not localhost)
2. Configure connection pooling (already set in code: max 25 connections)
3. Enable query logging for audit trails
4. Implement database read replicas for high availability
5. Use environment-specific configurations
6. Regularly update PostgreSQL to latest stable version
7. Monitor database performance and connection usage

## Next Steps

- [ ] Add user registration endpoint (POST /auth/register)
- [ ] Implement password reset functionality
- [ ] Add user management CRUD operations
- [ ] Create audit log table for authentication events
- [ ] Add session management table
- [ ] Implement role-based access control (RBAC) middleware
- [ ] Add email verification for new users
- [ ] Implement 2FA (Two-Factor Authentication)
