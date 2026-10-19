-- Individual episodes asked for (specials): JSON array of "S0E5"-style codes. A request
-- may carry seasons, episodes, or both.
ALTER TABLE requests ADD COLUMN episodes TEXT;
