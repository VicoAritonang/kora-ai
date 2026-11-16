"use client";
import { use } from "react";
import { JourneyStory } from "@/features/journeys/JourneyStory";

export default function JourneyPage({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = use(params);
  return <JourneyStory key={slug} slug={slug} />;
}
