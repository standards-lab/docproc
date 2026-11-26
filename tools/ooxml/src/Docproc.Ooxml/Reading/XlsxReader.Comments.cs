using DocumentFormat.OpenXml.Packaging;
using Docproc.Ooxml.Model;
using S = DocumentFormat.OpenXml.Spreadsheet;
using TC = DocumentFormat.OpenXml.Office2019.Excel.ThreadedComments;

namespace Docproc.Ooxml.Reading;

internal static partial class XlsxReader
{
    private sealed class SheetComments(WorkbookPart workbook)
    {
        private readonly Dictionary<string, string?> persons = Persons(workbook);
        private int lastId;

        /// <summary>Every thread read so far, in sheet order, its id and its replies' numbered from 1
        /// in the order they are read.</summary>
        public List<Comment> All { get; } = [];

        /// <summary>Reads a worksheet's threads, its legacy notes in their part's order and then each
        /// threaded comments part's threads in order, and returns each thread's id with the cell
        /// reference it anchors. A note on a cell a threaded comment anchors is the copy Excel writes
        /// of that comment for older readers, and is skipped.</summary>
        public List<(string Reference, int Id)> Read(WorksheetPart worksheet)
        {
            var anchored = new List<(string, int)>();
            List<TC.ThreadedComment> threaded = [.. worksheet.WorksheetThreadedCommentsParts
                .SelectMany(part => part.ThreadedComments?.Elements<TC.ThreadedComment>() ?? [])];
            var threadedCells = threaded.Select(c => Position(c.Ref?.Value ?? "")).OfType<(int, int)>().ToHashSet();
            var notes = worksheet.WorksheetCommentsPart?.Comments;
            List<string?> authors = [.. (notes?.Authors?.Elements<S.Author>() ?? []).Select(a => NonEmpty(a.Text))];
            foreach (var note in notes?.CommentList?.Elements<S.Comment>() ?? [])
            {
                var reference = note.Reference?.Value ?? "";
                if (Position(reference) is { } cell && threadedCells.Contains(cell))
                {
                    continue;
                }
                var author = note.AuthorId?.Value is uint index && index < authors.Count ? authors[(int)index] : null;
                var id = ++lastId;
                All.Add(new Comment(id, author, "", false, Paragraphs(note.CommentText is { } text ? Text(text) : null), []));
                anchored.Add((reference, id));
            }
            var threads = new List<(TC.ThreadedComment Root, List<TC.ThreadedComment> Replies)>();
            var byId = new Dictionary<string, int>(StringComparer.OrdinalIgnoreCase);
            foreach (var comment in threaded)
            {
                if (comment.ParentId?.Value is { } parent && byId.TryGetValue(parent, out var thread))
                {
                    threads[thread].Replies.Add(comment);
                }
                else
                {
                    thread = threads.Count;
                    threads.Add((comment, []));
                }
                if (comment.Id?.Value is { } own)
                {
                    byId.TryAdd(own, thread);
                }
            }
            foreach (var (root, replies) in threads)
            {
                var id = ++lastId;
                List<CommentReply> thread = [.. replies.Select(r => new CommentReply(++lastId, Person(r), ThreadedText(r)))];
                All.Add(new Comment(id, Person(root), "", root.Done?.Value == true, ThreadedText(root), thread));
                anchored.Add((root.Ref?.Value ?? "", id));
            }
            return anchored;
        }

        private string? Person(TC.ThreadedComment comment) =>
            comment.PersonId?.Value is { } id ? persons.GetValueOrDefault(id) : null;

        private static List<Block> ThreadedText(TC.ThreadedComment comment) =>
            Paragraphs(comment.ThreadedCommentText?.Text);

        private static List<Block> Paragraphs(string? text)
        {
            text = text?.ReplaceLineEndings("\n");
            return string.IsNullOrWhiteSpace(text) ? [] : [new Paragraph([new TextRun(text)])];
        }

        private static Dictionary<string, string?> Persons(WorkbookPart workbook)
        {
            var persons = new Dictionary<string, string?>(StringComparer.OrdinalIgnoreCase);
            foreach (var part in workbook.WorkbookPersonParts)
            {
                foreach (var person in part.PersonList?.Elements<TC.Person>() ?? [])
                {
                    if (person.Id?.Value is { } id)
                    {
                        persons.TryAdd(id, NonEmpty(person.DisplayName?.Value));
                    }
                }
            }
            return persons;
        }
    }
}
