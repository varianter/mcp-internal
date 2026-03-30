package tools

import (
	"context"
	"math/rand"

	"github.com/mark3labs/mcp-go/mcp"
)

var jokes = []string{
	// Programming
	"Why do programmers prefer dark mode? Because light attracts bugs.",
	"A SQL query walks into a bar, walks up to two tables and asks: 'Can I join you?'",
	"How many programmers does it take to change a light bulb? None, that's a hardware problem.",
	"Why do Java developers wear glasses? Because they don't C#.",
	"There are 10 types of people in the world: those who understand binary and those who don't.",
	"A programmer's partner says: 'Go to the store, get a gallon of milk, and if they have eggs, get a dozen.' The programmer comes home with 12 gallons of milk.",
	"Why was the JavaScript developer sad? Because he didn't Node how to Express himself.",
	"Git blame: the feature that turns coworkers into coworkers-who-avoid-eye-contact.",
	"It works on my machine. Ship the machine.",
	"'Merge conflict' — two words that ruin any developer's afternoon.",
	// IT / DevOps
	"I would tell you a joke about UDP, but you might not get it.",
	"Why did the developer go broke? Because he used up all his cache.",
	"Knock knock. Race condition. Who's there?",
	"To understand recursion, you must first understand recursion.",
	"The cloud is just someone else's computer. And someone else's problem.",
	"LGTM — Looks Good To Me (said without running the tests).",
	// Design
	"A UX designer walks into a bar. Bartender asks: 'The usual?' Designer says: 'Where's the affordance for that?'",
	"Clients: make the logo bigger. Also clients: why does nothing fit on the page?",
	"Designers don't make mistakes, they make happy little accidents that are still the client's fault.",
	"Comic Sans walks into a bar. The bartender says: 'We don't serve your type here.'",
	"Good design is invisible. So clients assume you did nothing.",
}

// NewRandomJokeTool returns a random IT, programming, or design joke.
func NewRandomJokeTool() (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("random-joke",
		mcp.WithDescription("Get a random IT, programming, or design joke to brighten your day"),
	)
	handler := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText(jokes[rand.Intn(len(jokes))]), nil
	}
	return tool, handler
}
