//! The content rule's alignment: a longest common subsequence of two token sequences, by Myers'
//! O((n + m)·D) algorithm with its linear-space refinement (E. W. Myers, "An O(ND) Difference
//! Algorithm and Its Variations", Algorithmica 1, 1986, section 4b), for n and m tokens and D
//! edits between them. The common prefix and suffix are trimmed first, then the middle snake of
//! an optimal path splits the rest in two, recursively. Where several longest common
//! subsequences tie, the rule's is the one this returns.
//!
//! The search is bounded: past a stated number of edits the alignment stops, so that the rule's
//! cost stays bounded for any input, and the caller compares by set membership instead.

use std::ops::Range;

/// Which of `a`'s tokens a longest common subsequence of `a` and `b` keeps, or None when more
/// than `edits_max` edits (deletions and insertions) separate them.
pub fn kept(a: &[u32], b: &[u32], edits_max: usize) -> Option<Vec<bool>> {
    let mut diff = Diff {
        a,
        b,
        kept: vec![false; a.len()],
        forward: V::new(depth(a.len(), b.len(), edits_max)),
        backward: V::new(depth(a.len(), b.len(), edits_max)),
    };
    diff.conquer(0..a.len(), 0..b.len(), Some(edits_max))
        .then_some(diff.kept)
}

/// The most rounds of the middle-snake search: each side's d reaches at most half of D, rounded
/// up, and no more than the sequences can need.
fn depth(n: usize, m: usize, edits_max: usize) -> usize {
    ((n + m).div_ceil(2) + 1).min(edits_max.div_ceil(2) + 1)
}

struct Diff<'a> {
    a: &'a [u32],
    b: &'a [u32],
    kept: Vec<bool>,
    forward: V,
    backward: V,
}

impl Diff<'_> {
    /// Aligns `a[x]` with `b[y]`, marking what it keeps; with `edits_max`, the top level, false
    /// when more edits than that separate them. A sub-problem's edits are within its parent's.
    fn conquer(
        &mut self,
        mut x: Range<usize>,
        mut y: Range<usize>,
        edits_max: Option<usize>,
    ) -> bool {
        while !x.is_empty() && !y.is_empty() && self.a[x.start] == self.b[y.start] {
            self.kept[x.start] = true;
            x.start += 1;
            y.start += 1;
        }
        while !x.is_empty() && !y.is_empty() && self.a[x.end - 1] == self.b[y.end - 1] {
            self.kept[x.end - 1] = true;
            x.end -= 1;
            y.end -= 1;
        }
        if x.is_empty() || y.is_empty() {
            return edits_max.is_none_or(|max| x.len() + y.len() <= max);
        }
        let Some((split_x, split_y)) = self.middle_snake(x.clone(), y.clone(), edits_max) else {
            return false;
        };
        self.conquer(x.start..split_x, y.start..split_y, None);
        self.conquer(split_x..x.end, split_y..y.end, None);
        true
    }

    /// Where an optimal path's middle snake starts, splitting the problem in two, or None when
    /// more than `edits_max` edits separate `a[x]` and `b[y]`. Both are non-empty and differ at
    /// both ends.
    fn middle_snake(
        &mut self,
        x: Range<usize>,
        y: Range<usize>,
        edits_max: Option<usize>,
    ) -> Option<(usize, usize)> {
        let (n, m) = (x.len(), y.len());
        let delta = n as isize - m as isize;
        let odd = delta & 1 == 1;
        self.forward.set(1, 0);
        self.backward.set(1, 0);
        let rounds = (n + m).div_ceil(2) + 1;
        // A path found in the forward search of round d has 2d - 1 edits, in the backward one 2d.
        let (forward_max, backward_max) = match edits_max {
            Some(max) => (max.div_ceil(2), max / 2),
            None => (rounds, rounds),
        };
        for d in 0..rounds.min(forward_max + 1) as isize {
            for k in (-d..=d).rev().step_by(2) {
                let mut at =
                    if k == -d || (k != d && self.forward.get(k - 1) < self.forward.get(k + 1)) {
                        self.forward.get(k + 1)
                    } else {
                        self.forward.get(k - 1) + 1
                    };
                let start = (at, (at as isize - k) as usize);
                let mut down = start.1;
                while at < n && down < m && self.a[x.start + at] == self.b[y.start + down] {
                    at += 1;
                    down += 1;
                }
                self.forward.set(k, at);
                if odd
                    && (k - delta).abs() < d
                    && self.forward.get(k) + self.backward.get(delta - k) >= n
                {
                    return Some((x.start + start.0, y.start + start.1));
                }
            }
            if d as usize > backward_max {
                continue;
            }
            for k in (-d..=d).rev().step_by(2) {
                let mut at =
                    if k == -d || (k != d && self.backward.get(k - 1) < self.backward.get(k + 1)) {
                        self.backward.get(k + 1)
                    } else {
                        self.backward.get(k - 1) + 1
                    };
                let mut up = (at as isize - k) as usize;
                while at < n
                    && up < m
                    && self.a[x.start + n - at - 1] == self.b[y.start + m - up - 1]
                {
                    at += 1;
                    up += 1;
                }
                self.backward.set(k, at);
                if !odd
                    && (k - delta).abs() <= d
                    && self.backward.get(k) + self.forward.get(delta - k) >= n
                {
                    return Some((x.start + n - at, y.start + m - up));
                }
            }
        }
        None
    }
}

/// The furthest x reached on each diagonal k, from -depth to depth.
struct V {
    offset: isize,
    x: Vec<usize>,
}

impl V {
    fn new(depth: usize) -> Self {
        Self {
            offset: depth as isize + 1,
            x: vec![0; 2 * depth + 3],
        }
    }

    fn get(&self, k: isize) -> usize {
        self.x[(k + self.offset) as usize]
    }

    fn set(&mut self, k: isize, x: usize) {
        self.x[(k + self.offset) as usize] = x;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// The length of a longest common subsequence, by dynamic programming, the reference.
    fn lcs(a: &[u32], b: &[u32]) -> usize {
        let mut row = vec![0usize; b.len() + 1];
        for &ai in a {
            let mut diagonal = 0;
            for (j, &bj) in b.iter().enumerate() {
                let up = row[j + 1];
                row[j + 1] = if ai == bj {
                    diagonal + 1
                } else {
                    up.max(row[j])
                };
                diagonal = up;
            }
        }
        row[b.len()]
    }

    /// Whether `kept` marks a subsequence of `a` that is one of `b`.
    fn is_common(a: &[u32], b: &[u32], kept: &[bool]) -> bool {
        let mut rest = b.iter();
        a.iter()
            .zip(kept)
            .filter(|(_, kept)| **kept)
            .all(|(token, _)| rest.any(|other| other == token))
    }

    /// A small deterministic generator, so the cases are the same on every run.
    fn sequence(seed: &mut u64, length: usize, alphabet: u32) -> Vec<u32> {
        (0..length)
            .map(|_| {
                *seed = seed
                    .wrapping_mul(6_364_136_223_846_793_005)
                    .wrapping_add(1_442_695_040_888_963_407);
                ((*seed >> 33) % u64::from(alphabet)) as u32
            })
            .collect()
    }

    /// Over many pairs, what it keeps is a common subsequence as long as the reference's.
    #[test]
    fn it_keeps_a_longest_common_subsequence() {
        let mut seed = 7;
        for round in 0..2_000 {
            let a = sequence(&mut seed, round % 40, 1 + round as u32 % 6);
            let b = sequence(&mut seed, (round * 7) % 37, 1 + round as u32 % 5);
            let kept = kept(&a, &b, usize::MAX).unwrap();
            assert!(is_common(&a, &b, &kept), "{a:?} {b:?} {kept:?}");
            assert_eq!(
                kept.iter().filter(|k| **k).count(),
                lcs(&a, &b),
                "{a:?} {b:?}"
            );
        }
    }

    #[test]
    fn edge_cases() {
        assert_eq!(kept(&[], &[], 0), Some(vec![]));
        assert_eq!(kept(&[1, 2], &[], 2), Some(vec![false, false]));
        assert_eq!(kept(&[1, 2, 3], &[1, 2, 3], 0), Some(vec![true; 3]));
        assert_eq!(kept(&[1, 9, 3], &[1, 3], 1), Some(vec![true, false, true]));
    }

    /// The bound is on D, the edits of an optimal alignment: exactly at it the alignment is
    /// made, one past it none is.
    #[test]
    fn the_bound_stops_the_alignment_past_it() {
        for edits in [1usize, 2, 3, 10, 11] {
            // Each differing pair is two edits, a deletion and an insertion; an extra token one.
            let a: Vec<u32> = (0..edits as u32 / 2)
                .flat_map(|n| [n * 3, n * 3 + 1])
                .chain((edits % 2 == 1).then_some(999))
                .collect();
            let b: Vec<u32> = (0..edits as u32 / 2)
                .flat_map(|n| [n * 3, n * 3 + 2])
                .collect();
            let d = a.len() + b.len() - 2 * lcs(&a, &b);
            assert_eq!(d, edits);
            assert!(kept(&a, &b, edits).is_some(), "{edits}");
            assert!(kept(&a, &b, edits - 1).is_none(), "{edits}");
        }
    }
}
