"use client";
import { use } from "react";
import { ConversationWorkspace } from "@/features/workspace/ConversationWorkspace";

export default function ConversationPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  return <ConversationWorkspace key={id} id={id} />;
}
