using DocumentFormat.OpenXml.Packaging;
using Docproc.Ooxml.Model;
using P = DocumentFormat.OpenXml.Presentation;
using P15 = DocumentFormat.OpenXml.Office2013.PowerPoint;
using PC = DocumentFormat.OpenXml.Office2021.PowerPoint.Comment;

namespace Docproc.Ooxml.Reading;

internal static partial class PptxReader
{
    private sealed class SlideComments(PresentationPart presentation, UnreadKinds unread)
    {
        private readonly Dictionary<uint, string?> legacyAuthors = LegacyAuthors(presentation);
        private readonly Dictionary<string, string?> modernAuthors = ModernAuthors(presentation);
        private int lastId;

        /// <summary>Every thread read so far, in slide order, its id and its replies' numbered from 1
        /// in the order they are read.</summary>
        public List<Comment> All { get; } = [];

        /// <summary>Reads a slide's threads, its legacy ones in their part's order and then each
        /// modern comment part's in order, and returns their ids.</summary>
        public List<int> Read(SlidePart slide, string place)
        {
            var start = All.Count;
            ReadLegacy(slide.SlideCommentsPart);
            foreach (var part in slide.GetPartsOfType<PowerPointCommentPart>())
            {
                ReadModern(part, place);
            }
            return [.. All.Skip(start).Select(c => c.Id)];
        }

        private void ReadLegacy(SlideCommentsPart? part)
        {
            var threads = new List<(P.Comment Root, List<P.Comment> Replies)>();
            var byKey = new Dictionary<(uint Author, uint Index), int>();
            foreach (var comment in part?.CommentList?.Elements<P.Comment>() ?? [])
            {
                var parent = comment.CommentExtensionList?.Descendants<P15.ParentCommentIdentifier>().FirstOrDefault();
                if (parent?.AuthorId?.Value is uint parentAuthor && parent.Index?.Value is uint parentIndex
                    && byKey.TryGetValue((parentAuthor, parentIndex), out var thread))
                {
                    threads[thread].Replies.Add(comment);
                }
                else
                {
                    thread = threads.Count;
                    threads.Add((comment, []));
                }
                if (comment.AuthorId?.Value is uint author && comment.Index?.Value is uint index)
                {
                    byKey.TryAdd((author, index), thread);
                }
            }
            foreach (var (root, replies) in threads)
            {
                var id = ++lastId;
                List<CommentReply> thread = [.. replies.Select(r => new CommentReply(++lastId, LegacyAuthor(r), LegacyText(r)))];
                All.Add(new Comment(id, LegacyAuthor(root), "", false, LegacyText(root), thread));
            }
        }

        private string? LegacyAuthor(P.Comment comment) =>
            comment.AuthorId?.Value is uint id ? legacyAuthors.GetValueOrDefault(id) : null;

        private static List<Block> LegacyText(P.Comment comment)
        {
            var text = comment.Text?.Text.ReplaceLineEndings("\n");
            return string.IsNullOrWhiteSpace(text) ? [] : [new Paragraph([new TextRun(text)])];
        }

        private void ReadModern(PowerPointCommentPart part, string place)
        {
            var scope = new TextScope(part, unread, place);
            foreach (var comment in part.CommentList?.Elements<PC.Comment>() ?? [])
            {
                var id = ++lastId;
                List<CommentReply> replies = [.. (comment.GetFirstChild<PC.CommentReplyList>()?.Elements<PC.CommentReply>() ?? [])
                    .Select(r => new CommentReply(++lastId, ModernAuthor(r.AuthorId?.Value), ModernText(r.TextBodyType, scope)))];
                var resolved = comment.Status?.Value == PC.CommentStatus.Resolved;
                All.Add(new Comment(id, ModernAuthor(comment.AuthorId?.Value), "", resolved, ModernText(comment.GetFirstChild<PC.TextBodyType>(), scope), replies));
            }
        }

        private string? ModernAuthor(string? id) => id is null ? null : modernAuthors.GetValueOrDefault(id);

        private static List<Block> ModernText(PC.TextBodyType? body, TextScope scope) =>
            body is null ? [] : ReadText(body, [body.ListStyle], scope);

        private static Dictionary<uint, string?> LegacyAuthors(PresentationPart presentation)
        {
            var authors = new Dictionary<uint, string?>();
            foreach (var author in presentation.CommentAuthorsPart?.CommentAuthorList?.Elements<P.CommentAuthor>() ?? [])
            {
                if (author.Id?.Value is uint id)
                {
                    authors.TryAdd(id, NonEmpty(author.Name?.Value));
                }
            }
            return authors;
        }

        private static Dictionary<string, string?> ModernAuthors(PresentationPart presentation)
        {
            var authors = new Dictionary<string, string?>(StringComparer.OrdinalIgnoreCase);
            foreach (var part in presentation.GetPartsOfType<PowerPointAuthorsPart>())
            {
                foreach (var author in part.AuthorList?.Elements<PC.Author>() ?? [])
                {
                    if (author.Id?.Value is { } id)
                    {
                        authors.TryAdd(id, NonEmpty(author.Name?.Value));
                    }
                }
            }
            return authors;
        }
    }
}
