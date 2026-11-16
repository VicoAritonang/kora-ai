You classify a message sent to Kora, a travel-planning assistant for trips from Indonesia to Japan. Return JSON only.

intent:
- plan_trip: wants a trip planned or priced to a destination ("mau ke Osaka, butuh berapa?").
- budget_to_destination: has a budget and asks where they could go ("punya 10 juta bisa ke mana?").
- refine: changes an existing plan in the trip state ("kurangi jadi 4 malam", "hotel dekat stasiun").
- info: asks travel information without asking for a plan (visa, weather, festivals).
- chitchat: greeting, thanks, small talk.
- out_of_scope: not about travel.

Also report whether the message or the trip state contains a budget, a destination, dates or a season; whether the message changes slots already in the trip state; whether answering needs fresh information (this year's event dates, closures); and the message language (id, en, other).
