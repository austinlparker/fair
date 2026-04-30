---
title: "Wide Events, Personal Software, and You."
date: "2025-04-17"
---

I recently built a fun little project called [777-BSKY](https://777bsky.fly.dev). It looks at Bluesky trending topics, does some math, figures out what's most popular and slaps some TTS on it. You can call a phone number and have the output read back to you, kinda like Moviefone except all of the movies are talking about the twilight of the American experiment.

That said, I'm actually not writing this to talk about the project, but a realization I had while writing it about observability. Specifically, this project made me realize the value of wide events and where they fit into software.

## What's a Wide Event?

It's what it sounds like, more or less. A single structured log with tens, hundreds, or thousands of dimensions and practically infinite cardinality on those dimensions. I've long been skeptical of wide events for production/line of business systems for a few reasons:

1. Most production systems are _complex_, and understanding performance requires understanding the relationship between dependencies. The interesting stuff in your system is often obscured through layers of abstraction, even in a single service, and a single event per service often misses useful stuff.

2. Data hygiene matters a lot in production. Semantic drift is a real pain to deal with, and standardizing metadata on events either requires that you own your entire stack (vanishingly likely unless you're in an extremely large organization that can dedicate people to internal framework development), or that you don't really care about what's happening outside of your team (which is a Conway's Law shaped problem). When other people own your instrumentation, you don't really get a chance to say what they should use, and you can't rely on everyone else adopting your data model.

3. Events, by themselves, don't imply relationships. I think a lot of folks would like to have their cake and eat it too when it comes to the relationship between spans and events. Spans have some very explicit guarantees around both duration and heirarchies that events, by themselves, lack. When you're working with a distributed system, these are very nice guarantees to have!

There's a secret, bonus, fourth thing that I think really devalues wide events -- they're good in prod and bad in dev. Even for highly async code, your mental model of a program is usually a linear one; Maybe a tree, with branches flying this way and that, but fundamentally you think of things as beginning and ending. You start a loop, you call some functions, there's an order to it that's extremely appealing to the part of my brain that likes lining up all of the sheets of paper in a stack. Wide events, more or less, smoosh this stack down into one (or a handful) of aggregates. I don't need a billion dimensions when I'm writing software on my laptop; Most of those dimensions are known, because I control them! Logging's enduring popularity is buttressed by this local development loop. Traces are somewhat better here, although the local development experience with them is still pretty bad -- however, you can more easily realize value from it through local visualizations. Wide Events? They sit in a weird spot in this heirarchy. If you think of them as spans and traces, then why go wide? Create them at logical boundaries in the code rather than at the oh-so-arbitrary cliff of a 'service'. If you think of them as structured logs, then it's a little better -- but you're not really getting all of the benefits since your debugging data is locked behind debug or trace level loginfo that will never get turned on in production.

I could probably write a whole book about the failures of the observability tooling space and their inability to solve developer pain points, but that's a different blog.

## I Write Events Not Tragedies

Wide Events are a painterly construct more than an industrial one. Consider that electrification took decades to become truly ubitquious; Industrial adoption of electricity was concomitant with the production line. Thankfully we have a very good example of this in the software industry today -- vibe coding, and the return of personal software.

What do I mean by 'personal software'? What it says on the tin. Software that you write for yourself to solve your problems. I think in many ways we'll see the early 2020's as the apex of Big Platform -- massive, sprawling, centralized suites that you lived your digital life out of. AI lowers the barrier to entry dramatically for individuals to write software that solves _their_ needs, fit to _their_ use cases, and built to _their_ requirements.

This does mean, however, that we'll need better ways to observe that software. We'll have an entire new generation of developers, running code and dealing with operations for the first time. We'll need clear, explainable, and idiomatic ways of describing what this software does and how it fits together. This, I think, is where we'll discover the value of wide events.

If you look at 777-BSKY, it uses tracing, but not in the way you'd expect. It's not building deep and complex traces for every operation; Most of them only emit a single span. It has detailed logging for local development as well. I think it's a lot more useful this way, though! I didn't need to putz around with the AI and have it create metrics, or complex traces. It was actually a lot easier to tell it "hey, I just want a single span per operation" and it went and created a little helper library for it. Ironically enough, it probably had a better idea of how to create 'wide events' because what writing exists on it is much more focused.

## Putting It Together

Honestly, I have more questions than answers at the end of this project. I'm more concerned than I was before about the accessibility of observability tooling to new developers. I believe the real challenge arising from AI assistance is going to be around deployment and operation of code, moreso than the creation and maintenence of it. I'm increasingly convinced that we're in the twilight of platforms for everything from social networking to business suites and CRMs. I still don't think wide events are that useful for most business software -- but I think they might be, because business software is also going to change. Whatever comes next, it's gonna be interesting.
