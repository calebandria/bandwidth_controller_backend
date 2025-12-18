#!/bin/bash

# Database Setup Script for QoS Bandwidth Controller
# This script sets up PostgreSQL database with users table

set -e

echo "==================================="
echo "QoS Bandwidth Database Setup"
echo "==================================="

# Configuration
DB_NAME="qos_bandwidth"
DB_USER="qos_user"
DB_PASSWORD="qos_password"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if PostgreSQL is installed
if ! command -v psql &> /dev/null; then
    echo -e "${RED}PostgreSQL is not installed!${NC}"
    echo "Install it with: sudo apt install postgresql postgresql-contrib"
    exit 1
fi

echo -e "${GREEN}PostgreSQL is installed${NC}"

# Check if PostgreSQL service is running
if ! sudo systemctl is-active --quiet postgresql; then
    echo -e "${YELLOW}Starting PostgreSQL service...${NC}"
    sudo systemctl start postgresql
    sudo systemctl enable postgresql
fi

echo -e "${GREEN}PostgreSQL service is running${NC}"

# Create database and user
echo -e "${YELLOW}Creating database and user...${NC}"
sudo -u postgres psql <<EOF
-- Create database if not exists
SELECT 'CREATE DATABASE $DB_NAME' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '$DB_NAME')\gexec

-- Create user if not exists
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_user WHERE usename = '$DB_USER') THEN
    CREATE USER $DB_USER WITH PASSWORD '$DB_PASSWORD';
  END IF;
END
\$\$;

-- Grant privileges
GRANT ALL PRIVILEGES ON DATABASE $DB_NAME TO $DB_USER;
EOF

echo -e "${GREEN}Database and user created${NC}"

# Grant schema privileges
echo -e "${YELLOW}Granting schema privileges...${NC}"
sudo -u postgres psql -d $DB_NAME <<EOF
GRANT ALL ON SCHEMA public TO $DB_USER;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO $DB_USER;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO $DB_USER;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO $DB_USER;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO $DB_USER;
EOF

echo -e "${GREEN}Schema privileges granted${NC}"

# Run migrations
echo -e "${YELLOW}Running database migrations...${NC}"
if [ -f "$SCRIPT_DIR/migrations/001_create_users_table.sql" ]; then
    sudo -u postgres psql -d $DB_NAME -f "$SCRIPT_DIR/migrations/001_create_users_table.sql"
    echo -e "${GREEN}Migrations completed successfully${NC}"
else
    echo -e "${RED}Migration file not found: $SCRIPT_DIR/migrations/001_create_users_table.sql${NC}"
    exit 1
fi

# Verify setup
echo -e "${YELLOW}Verifying setup...${NC}"
RESULT=$(sudo -u postgres psql -d $DB_NAME -t -c "SELECT COUNT(*) FROM users;")
USER_COUNT=$(echo $RESULT | tr -d ' ')

if [ "$USER_COUNT" -ge "2" ]; then
    echo -e "${GREEN}Setup verified! Found $USER_COUNT users in database${NC}"
else
    echo -e "${RED}Setup verification failed! Expected at least 2 users, found $USER_COUNT${NC}"
    exit 1
fi

# Display users
echo -e "${YELLOW}Current users in database:${NC}"
sudo -u postgres psql -d $DB_NAME -c "SELECT id, username, role, name, created_at FROM users;"

echo ""
echo -e "${GREEN}==================================="
echo "Database setup completed successfully!"
echo "===================================${NC}"
echo ""
echo "Database credentials:"
echo "  Database: $DB_NAME"
echo "  User: $DB_USER"
echo "  Password: $DB_PASSWORD"
echo ""
echo "Default login credentials:"
echo "  Admin - username: admin, password: admin123"
echo "  User  - username: user, password: user123"
echo ""
echo -e "${YELLOW}⚠️  IMPORTANT: Change default passwords before deploying to production!${NC}"
echo ""
echo "Set environment variables before running the application:"
echo "  export DB_HOST=localhost"
echo "  export DB_PORT=5432"
echo "  export DB_USER=$DB_USER"
echo "  export DB_PASSWORD=$DB_PASSWORD"
echo "  export DB_NAME=$DB_NAME"
echo "  export DB_SSLMODE=disable"
echo ""
