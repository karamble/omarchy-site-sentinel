# Site Sentinel recipes

One per operator, with the command, what makes it fire, and the line delivered.

## A site goes down

    sentinel arm -path sites.downList -op appears -expires 30d -standing \
      -reason "shop is revenue critical" -armed-by ops

Fires when a site enters the down list, which takes two consecutive failed
rounds. Standing, so it rings for each site that falls over.

    sentinel alarm 4c81f0: sites.downList appeared: shop.example.com

## One particular site goes down

    sentinel arm -path sites.downList -op appears -expires 30d -standing \
      -where host=shop.example.com

Same trigger, narrowed. `-where` takes `field=value` for an exact match and
`field~=text` for a substring.

## A site comes back

    sentinel arm -path sites.downList -op disappears -expires 7d \
      -where host=shop.example.com -reason "tell me when the migration lands"

## A certificate gets close to expiry

    sentinel arm -path sites.worstCertDays -op crosses -below 10 -expires 90d \
      -reason "renew before the weekend"

Fires when the certificate closest to expiry drops under ten days. Certificates
renewing at thirty days do not trip it.

## Any certificate becomes invalid

    sentinel arm -path sites.certProblems -op appears -expires 90d -standing

Covers expired, wrong host and broken chain, which a days-remaining trigger
misses entirely.

## More than two sites down at once

    sentinel arm -path sites.down -op crosses -above 2 -expires 30d \
      -reason "several at once usually means the host, not the sites"

## A domain approaches renewal

    sentinel arm -path sites.domainProblems -op appears -expires 365d -standing

Domains with no published expiry never enter this list, so it stays quiet
rather than crying wolf about them.

## A site moves host or nameserver

    sentinel arm -path sites.moved -op appears -expires 90d -standing \
      -reason "nobody should be moving these without telling me"

## A site has been down for a while

    sentinel arm -path sites.downList -op ages -older-than 2h -field since \
      -expires 30d -reason "escalate if it is still down after two hours"

## This machine loses connectivity

    sentinel arm -path health.localFault -op becomes -value true -expires 30d

Useful once: while this is true, no site result means anything.
