-- CreateEnum
CREATE TYPE "SessionMode" AS ENUM ('planning', 'interactive', 'auto');

-- CreateEnum
CREATE TYPE "SessionStatus" AS ENUM ('idle', 'running', 'archived');

-- CreateEnum
CREATE TYPE "McpServerType" AS ENUM ('stdio', 'http');

-- CreateTable
CREATE TABLE "User" (
    "id" TEXT NOT NULL,
    "githubId" TEXT NOT NULL,
    "login" TEXT NOT NULL,
    "name" TEXT,
    "avatarUrl" TEXT,
    "encryptedAccessToken" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "User_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Session" (
    "id" TEXT NOT NULL,
    "userId" TEXT NOT NULL,
    "title" TEXT NOT NULL,
    "repoFullName" TEXT NOT NULL,
    "repoDefaultBranch" TEXT NOT NULL,
    "mode" "SessionMode" NOT NULL DEFAULT 'interactive',
    "status" "SessionStatus" NOT NULL DEFAULT 'idle',
    "sdkSessionId" TEXT,
    "lastEventSeq" INTEGER NOT NULL DEFAULT 0,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,
    "lastActiveAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "Session_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "SessionAgent" (
    "id" TEXT NOT NULL,
    "sessionId" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "displayName" TEXT,
    "description" TEXT,
    "prompt" TEXT NOT NULL,
    "tools" TEXT[],
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "SessionAgent_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "SessionSkill" (
    "id" TEXT NOT NULL,
    "sessionId" TEXT NOT NULL,
    "skillName" TEXT NOT NULL,
    "enabled" BOOLEAN NOT NULL DEFAULT true,

    CONSTRAINT "SessionSkill_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "SessionMcpServer" (
    "id" TEXT NOT NULL,
    "sessionId" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "type" "McpServerType" NOT NULL,
    "target" TEXT NOT NULL,
    "encryptedConfig" TEXT,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "SessionMcpServer_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "SessionFunction" (
    "id" TEXT NOT NULL,
    "sessionId" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "description" TEXT NOT NULL,
    "parametersSchema" JSONB NOT NULL,
    "webhookUrl" TEXT NOT NULL,
    "encryptedHeaders" TEXT,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "SessionFunction_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "SessionEvent" (
    "id" TEXT NOT NULL,
    "sessionId" TEXT NOT NULL,
    "seq" INTEGER NOT NULL,
    "type" TEXT NOT NULL,
    "data" JSONB NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "SessionEvent_pkey" PRIMARY KEY ("id")
);

-- CreateIndex
CREATE UNIQUE INDEX "User_githubId_key" ON "User"("githubId");

-- CreateIndex
CREATE INDEX "Session_userId_idx" ON "Session"("userId");

-- CreateIndex
CREATE UNIQUE INDEX "SessionAgent_sessionId_name_key" ON "SessionAgent"("sessionId", "name");

-- CreateIndex
CREATE UNIQUE INDEX "SessionSkill_sessionId_skillName_key" ON "SessionSkill"("sessionId", "skillName");

-- CreateIndex
CREATE UNIQUE INDEX "SessionMcpServer_sessionId_name_key" ON "SessionMcpServer"("sessionId", "name");

-- CreateIndex
CREATE UNIQUE INDEX "SessionFunction_sessionId_name_key" ON "SessionFunction"("sessionId", "name");

-- CreateIndex
CREATE INDEX "SessionEvent_sessionId_seq_idx" ON "SessionEvent"("sessionId", "seq");

-- CreateIndex
CREATE UNIQUE INDEX "SessionEvent_sessionId_seq_key" ON "SessionEvent"("sessionId", "seq");

-- AddForeignKey
ALTER TABLE "Session" ADD CONSTRAINT "Session_userId_fkey" FOREIGN KEY ("userId") REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "SessionAgent" ADD CONSTRAINT "SessionAgent_sessionId_fkey" FOREIGN KEY ("sessionId") REFERENCES "Session"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "SessionSkill" ADD CONSTRAINT "SessionSkill_sessionId_fkey" FOREIGN KEY ("sessionId") REFERENCES "Session"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "SessionMcpServer" ADD CONSTRAINT "SessionMcpServer_sessionId_fkey" FOREIGN KEY ("sessionId") REFERENCES "Session"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "SessionFunction" ADD CONSTRAINT "SessionFunction_sessionId_fkey" FOREIGN KEY ("sessionId") REFERENCES "Session"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "SessionEvent" ADD CONSTRAINT "SessionEvent_sessionId_fkey" FOREIGN KEY ("sessionId") REFERENCES "Session"("id") ON DELETE CASCADE ON UPDATE CASCADE;
