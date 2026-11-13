using System.Text;
using DocumentFormat.OpenXml.Packaging;
using SpikeAiPipeline.Office.Model;
using W = DocumentFormat.OpenXml.Wordprocessing;
using W15 = DocumentFormat.OpenXml.Office2013.Word;

namespace SpikeAiPipeline.Office.Reading;

// The state one docx conversion shares across its story parts: the styles and numbering, the
// media found so far, the section the body is in, the notes the text has referenced, and the
// comments with the ranges they are anchored to.
internal static partial class DocxReader
{
    private sealed class DocxContext(MainDocumentPart main, Styles styles, Numbering numbering)
    {
        public MainDocumentPart Main { get; } = main;

        public Styles Styles { get; } = styles;

        public Numbering Numbering { get; } = numbering;

        public MediaStore Media { get; } = new();

        /// <summary>The section index media are placed in: the body's current section, the
        /// section a header or footer is read for, or null for a comment.</summary>
        public int? Section { get; set; } = 0;

        public NoteSet Footnotes { get; } = new(main.FootnotesPart?.Footnotes?.Elements<W.Footnote>(), main.FootnotesPart);

        public NoteSet Endnotes { get; } = new(main.EndnotesPart?.Endnotes?.Elements<W.Endnote>(), main.EndnotesPart);

        public CommentSet Comments { get; } = new(main);

        /// <summary>What the conversion skipped, each a line for stderr.</summary>
        public List<string> Warnings { get; } = [];
    }

    /// <summary>A notes part's footnotes or endnotes, each read when the text first references
    /// it. Word's separator and continuation notes are no notes of the text.</summary>
    private sealed class NoteSet
    {
        private readonly Dictionary<int, W.FootnoteEndnoteType> notes = [];
        private readonly HashSet<int> referenced = [];
        private readonly OpenXmlPart? part;

        public NoteSet(IEnumerable<W.FootnoteEndnoteType>? notes, OpenXmlPart? part)
        {
            this.part = part;
            foreach (var note in notes ?? [])
            {
                var type = note.Type?.Value;
                if (note.Id?.Value is long id
                    && type != W.FootnoteEndnoteValues.Separator
                    && type != W.FootnoteEndnoteValues.ContinuationSeparator
                    && type != W.FootnoteEndnoteValues.ContinuationNotice)
                {
                    this.notes.TryAdd((int)id, note);
                }
            }
        }

        /// <summary>The notes read, in the order the text first referenced them.</summary>
        public List<Note> Read { get; } = [];

        /// <summary>Reads the note on its first reference; returns whether there is such a note,
        /// so a reference to none is dropped. Its media are placed in the referencing section.</summary>
        public bool Reference(DocxContext context, int id)
        {
            if (part is null || !notes.TryGetValue(id, out var note))
            {
                return false;
            }
            if (referenced.Add(id))
            {
                // A note the note references comes after it.
                var index = Read.Count;
                var blocks = new BodyReader(context, part, isBody: false).ReadBlocks(note);
                Read.Insert(index, new Note(id, blocks));
            }
            return true;
        }
    }

    /// <summary>The comments part's comments, as threads: a comment whose commentsExtended entry
    /// names a parent is a reply in its root's thread, and a thread is resolved when its root's
    /// entry is done. Without commentsExtended every comment is a thread of its own, unresolved.
    /// The body reader gives each thread's root its anchor, the accepted text between its range's
    /// start and end, and marks where the range ends, or where its reference is when the range
    /// ends between paragraphs.</summary>
    private sealed class CommentSet
    {
        private readonly OpenXmlPart? part;
        private readonly List<W.Comment> roots = [];
        private readonly HashSet<string> rootIds = new(StringComparer.Ordinal);
        private readonly Dictionary<string, List<W.Comment>> replies = new(StringComparer.Ordinal);
        private readonly Dictionary<string, bool> resolved = new(StringComparer.Ordinal);
        private readonly Dictionary<string, StringBuilder> open = new(StringComparer.Ordinal);
        private readonly Dictionary<string, string> anchors = new(StringComparer.Ordinal);
        private readonly HashSet<string> marked = new(StringComparer.Ordinal);

        public CommentSet(MainDocumentPart main)
        {
            part = main.WordprocessingCommentsPart;
            var comments = main.WordprocessingCommentsPart?.Comments?.Elements<W.Comment>().Where(c => c.Id?.Value is not null).ToList() ?? [];
            var extended = new Dictionary<string, W15.CommentEx>(StringComparer.OrdinalIgnoreCase);
            foreach (var entry in main.WordprocessingCommentsExPart?.CommentsEx?.Elements<W15.CommentEx>() ?? [])
            {
                if (entry.ParaId?.Value is { } paraId)
                {
                    extended.TryAdd(paraId, entry);
                }
            }
            // A comment's commentsExtended entry is its last paragraph's.
            var byParagraph = new Dictionary<string, W.Comment>(StringComparer.OrdinalIgnoreCase);
            foreach (var comment in comments)
            {
                if (ParagraphId(comment) is { } paraId)
                {
                    byParagraph.TryAdd(paraId, comment);
                }
            }
            W15.CommentEx? Entry(W.Comment comment) => ParagraphId(comment) is { } id ? extended.GetValueOrDefault(id) : null;
            W.Comment Root(W.Comment comment)
            {
                var seen = new HashSet<W.Comment>();
                while (seen.Add(comment)
                    && Entry(comment)?.ParaIdParent?.Value is { } parentId
                    && byParagraph.TryGetValue(parentId, out var parent))
                {
                    comment = parent;
                }
                return comment;
            }
            foreach (var comment in comments)
            {
                var root = Root(comment);
                var id = comment.Id!.Value!;
                if (ReferenceEquals(root, comment))
                {
                    roots.Add(comment);
                    rootIds.Add(id);
                    replies[id] = [];
                    resolved[id] = Entry(comment)?.Done?.Value ?? false;
                }
                else
                {
                    replies[root.Id!.Value!].Add(comment);
                }
            }
        }

        private static string? ParagraphId(W.Comment comment) =>
            comment.Elements<W.Paragraph>().LastOrDefault()?.ParagraphId?.Value;

        public void Start(string? id)
        {
            if (id is not null && rootIds.Contains(id) && !anchors.ContainsKey(id))
            {
                open.TryAdd(id, new StringBuilder());
            }
        }

        /// <summary>Ends a range, its anchor the text read since it started.</summary>
        public void Close(string? id)
        {
            if (id is not null && open.Remove(id, out var anchor))
            {
                anchors[id] = anchor.ToString().TrimEnd('\n');
            }
        }

        /// <summary>A comment's reference; returns the comment to mark here when its root has no
        /// mark yet, as a comment with no range has none.</summary>
        public int? Point(string? id) =>
            id is not null && rootIds.Contains(id) && int.TryParse(id, out var number) && marked.Add(id) ? number : null;

        public void Append(string text)
        {
            foreach (var anchor in open.Values)
            {
                anchor.Append(text);
            }
        }

        // A range across paragraphs keeps their break.
        public void EndParagraph()
        {
            foreach (var anchor in open.Values)
            {
                if (anchor.Length > 0)
                {
                    anchor.Append('\n');
                }
            }
        }

        public List<Comment> Build(DocxContext context)
        {
            var result = new List<Comment>();
            if (part is null)
            {
                return result;
            }
            foreach (var root in roots)
            {
                var id = root.Id!.Value!;
                if (!int.TryParse(id, out var number))
                {
                    continue;
                }
                var thread = new List<CommentReply>();
                foreach (var reply in replies[id])
                {
                    if (int.TryParse(reply.Id!.Value, out var replyNumber))
                    {
                        thread.Add(new CommentReply(replyNumber, NonEmpty(reply.Author?.Value), Blocks(context, reply)));
                    }
                }
                result.Add(new Comment(number, NonEmpty(root.Author?.Value), anchors.GetValueOrDefault(id, ""), resolved[id], Blocks(context, root), thread));
            }
            return result;
        }

        private List<Block> Blocks(DocxContext context, W.Comment comment) =>
            new BodyReader(context, part!, isBody: false).ReadBlocks(comment);
    }
}
