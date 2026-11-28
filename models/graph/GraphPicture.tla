--------------------------- MODULE GraphPicture ---------------------------
(* What one stored graph is allowed to draw.

   GraphStore checks a write. It does not say which stored subjects become
   nodes. A phrase is not a node. A subject with no stored link is not drawn,
   including the single subject an empty graph accepts. A real subject that
   has a stored link stays drawn when the other end is a phrase: hiding the
   phrase must not hide its neighbor.

   DropPhraseNeighbor is the panel filter that used to require both ends to
   survive. It drops a real subject linked only to a phrase. DrawPhrase paints
   the phrase itself. *)
CONSTANTS DropPhraseNeighbor, DrawPhrase

Subjects == {"a", "b", "p"}
Reals == {"a", "b"}
Phrase == "p"

VARIABLES stored, links

vars == <<stored, links>>

LinkId(x, y) ==
    IF (x = "a" /\ y = "b") \/ (x = "b" /\ y = "a") THEN "ab"
    ELSE IF (x = "a" /\ y = "p") \/ (x = "p" /\ y = "a") THEN "ap"
    ELSE "bp"

Init == stored = {} /\ links = {}

Neighbor(s) == {t \in stored : t # s /\ LinkId(s, t) \in links}
RealNeighbor(s) == {t \in Neighbor(s) : t # Phrase}

Drawn(s) ==
    /\ s \in stored
    /\ IF s = Phrase THEN DrawPhrase
       ELSE IF DropPhraseNeighbor THEN RealNeighbor(s) # {}
       ELSE Neighbor(s) # {}

StoreSole ==
    /\ stored = {}
    /\ \E s \in Reals : stored' = {s}
    /\ UNCHANGED links

StoreRealLink ==
    /\ stored = {}
    /\ stored' = {"a", "b"}
    /\ links' = {"ab"}

StorePhraseLink ==
    /\ stored = {}
    /\ stored' = {"a", "p"}
    /\ links' = {"ap"}

StoreBoth ==
    /\ stored = {}
    /\ stored' = {"a", "b", "p"}
    /\ links' = {"ab", "ap"}

Next == StoreSole \/ StoreRealLink \/ StorePhraseLink \/ StoreBoth

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ stored \subseteq Subjects
    /\ links \subseteq {"ab", "ap", "bp"}

PhraseHidden == ~Drawn(Phrase)
LinkedRealStaysVisible ==
    \A s \in Reals : (s \in stored /\ Neighbor(s) # {}) => Drawn(s)

NeverHidesSole == \A s \in Reals : ~(stored = {s} /\ links = {} /\ ~Drawn(s))
NeverDrawsPhraseNeighbor ==
    ~(stored = {"a", "p"} /\ links = {"ap"} /\ Drawn("a"))

=============================================================================
