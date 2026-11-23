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

    fn middle_snake(
        &mut self,
        x: Range<usize>,
        y: Range<usize>,
        edits_max: Option<usize>,
    ) -> Option<(usize, usize)> {
        let a = &self.a[x.clone()];
        let b = &self.b[y.clone()];
        let (n, m) = (a.len(), b.len());
        let delta = n as isize - m as isize;
        let odd = delta & 1 == 1;
        let forward = &mut self.forward;
        let backward = &mut self.backward;
        forward.set(1, 0);
        backward.set(1, 0);
        let rounds = (n + m).div_ceil(2) + 1;
        let (forward_max, backward_max) = match edits_max {
            Some(max) => (max.div_ceil(2), max / 2),
            None => (rounds, rounds),
        };
        for d in 0..rounds.min(forward_max + 1) as isize {
            let mut k = d;
            let mut above = 0;
            while k >= -d {
                let start = furthest_start(forward, k, d, &mut above);
                let down = (start as isize - k) as usize;
                let (mut at, mut y_at) = (start, down);
                while at < n && y_at < m && a[at] == b[y_at] {
                    at += 1;
                    y_at += 1;
                }
                forward.set(k, at);
                if odd && (k - delta).abs() < d && at + backward.get(delta - k) >= n {
                    return Some((x.start + start, y.start + down));
                }
                k -= 2;
            }
            if d as usize > backward_max {
                continue;
            }
            let mut k = d;
            let mut above = 0;
            while k >= -d {
                let start = furthest_start(backward, k, d, &mut above);
                let (mut at, mut up) = (start, (start as isize - k) as usize);
                while at < n && up < m && a[n - at - 1] == b[m - up - 1] {
                    at += 1;
                    up += 1;
                }
                backward.set(k, at);
                if !odd && (k - delta).abs() <= d && at + forward.get(delta - k) >= n {
                    return Some((x.start + n - at, y.start + m - up));
                }
                k -= 2;
            }
        }
        None
    }
}

fn furthest_start(v: &V, k: isize, d: isize, above: &mut usize) -> usize {
    if k == -d {
        return *above;
    }
    let below = v.get(k - 1);
    let start = if k == d {
        below + 1
    } else {
        (below + 1).max(*above)
    };
    *above = below;
    start
}

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

    fn is_common(a: &[u32], b: &[u32], kept: &[bool]) -> bool {
        let mut rest = b.iter();
        a.iter()
            .zip(kept)
            .filter(|(_, kept)| **kept)
            .all(|(token, _)| rest.any(|other| other == token))
    }

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
    fn ties_keep_the_rules_choice() {
        let cases: [(&[u32], &[u32], [u8; 6]); 6] = [
            (&[1, 2, 1, 2], &[2, 1, 2, 1], [1, 1, 1, 0, 0, 0]),
            (&[1, 2, 3], &[3, 2, 1], [1, 0, 0, 0, 0, 0]),
            (&[1, 1, 2, 2], &[2, 2, 1, 1], [1, 1, 0, 0, 0, 0]),
            (&[1, 2, 1, 3, 1], &[1, 3, 1, 2, 1], [1, 1, 0, 0, 1, 0]),
            (&[5, 6, 7, 8], &[7, 8, 5, 6], [1, 1, 0, 0, 0, 0]),
            (
                &[1, 2, 3, 4, 1, 2],
                &[3, 4, 1, 2, 9, 3, 4],
                [0, 0, 1, 1, 1, 1],
            ),
        ];
        for (a, b, want) in cases {
            let want: Vec<bool> = want[..a.len()].iter().map(|k| *k == 1).collect();
            assert_eq!(kept(a, b, usize::MAX), Some(want), "{a:?} {b:?}");
        }
    }

    #[test]
    fn edge_cases() {
        assert_eq!(kept(&[], &[], 0), Some(vec![]));
        assert_eq!(kept(&[1, 2], &[], 2), Some(vec![false, false]));
        assert_eq!(kept(&[1, 2, 3], &[1, 2, 3], 0), Some(vec![true; 3]));
        assert_eq!(kept(&[1, 9, 3], &[1, 3], 1), Some(vec![true, false, true]));
    }

    #[test]
    fn the_bound_stops_the_alignment_past_it() {
        for edits in [1usize, 2, 3, 10, 11] {
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
