-- Migration 003: add archived column to projects
-- Archived projects are hidden from the sidebar but retained in the database.
ALTER TABLE projects ADD COLUMN archived INTEGER NOT NULL DEFAULT 0;
